# Bridge(双向字节泵)专题调研

> 2026-09-10。回答三个问题:Bridge 是什么、各开源项目怎么实现这个概念、我们取什么舍什么。
> 配合 `docs/design.md` §二 的 Bridge 角色定义与半关闭传播矩阵阅读。看开源项目时按 §四 的路径对照源码。

## 一、概念:所有代理的最小公共内核

Bridge 连接的是**两条已经建立好的连接**——它不拨号(Ingress/Listener 收)、不选路(Router)、不解析协议(Handler)、不建目标连接(Dialer),只把两条全双工字节流接在一起双向搬运,并管理关闭语义。

```
Ingress 收下用户连接(src) → Handler 解析协议 → Router 选路 → Dialer.Dial 建立目标连接(dst)
                                                            ↓
                                                 Bridge.Bridge(src, dst)
```

职责口诀:**Dialer 负责"怎么建立",Bridge 负责"建立之后"**;前者知道目标和身份,后者最好什么都不知道。"协议转换网关"的全部转换,最终都坍缩成这个哑字节泵;它的复杂性全在"哑"字里:关闭语义、背压、记账、限速。

## 二、业界同名物对照

每个项目都有一个,名字不同,本质同一个函数:

| 项目 | 名字 | 形状一句话 |
|------|------|-----------|
| gost v3 | `net.Transport(rw1, rw2)` | 纯函数,双 goroutine,池化 buffer |
| frp | `util.Join(c1, c2 io.ReadWriteCloser)` | 最简双向拷贝,一断全断 |
| nps | `conn.CopyWaitGroup(conn1, conn2, ...)` | 8 参数"上帝函数" |
| v2ray / sing-box | `Link` / `Relay`(buf 管道对接) | 内部缓冲管道 + 事件通知,自研链路 |
| Envoy | `tcp_proxy` network filter | 显式水位线背压(high/low watermark) |
| HAProxy | tunnel 模式转发 | 同上,watermark 流派 |
| socat | 程序本体 | 双地址类型间 relay,概念鼻祖 |
| ssh(-L/-R) | channel ↔ TCP 转发 | 自带 SSH 窗口流控的拷贝 |

## 三、源码证据(本地已克隆,路径可直读)

### gost 的干净版(精华样本)

`test/example/gost-x/internal/net/transport.go`:

```go
const bufferSize = 64 * 1024

func Transport(rw1, rw2 io.ReadWriter) error {
    errc := make(chan error, 2)
    go func() { errc <- CopyBuffer(rw1, rw2, bufferSize) }()
    go func() { errc <- CopyBuffer(rw2, rw1, bufferSize) }()
    if err := <-errc; err != nil && err != io.EOF {
        return err
    }
    return nil  // 调用方 defer 关闭两端,剩余 goroutine 随之退出
}

// CopyBuffer: 从 bufpool 取 buffer 复用,降低分配开销
func CopyBuffer(dst io.Writer, src io.Reader, bufSize int) error {
    buf := bufpool.Get(bufSize)
    defer bufpool.Put(buf)
    _, err := io.CopyBuffer(dst, src, buf)
    return err
}
```

要点:零业务参数、64KB 池化 buffer、errc 带缓冲、第一个非 EOF 错误胜出。

### nps 的膨胀版(糟粕样本)

`test/example/nps/lib/conn/conn.go` 的 `CopyWaitGroup`:

```go
func CopyWaitGroup(conn1, conn2 net.Conn, crypt bool, snappy bool,
                   rate *rate.Rate, flow *file.Flow, isServer bool, rb []byte)
```

- 8 个参数:加密包装、snappy 压缩、限速器、流量统计、**方向标志 isServer**(包装器依赖调用方告知上下文)、`rb` = 嗅探阶段已读出的首包重放;
- 函数体内留着一大段注释掉的上一版实现(死代码);
- 转发经 goroutine 池(`goroutine.CopyConnsPool`)派发,流量记账耦合在池任务里。

### frp 的基线版(未克隆,看源码时读)

`pkg/util/net` 里的 `Join`:两个 goroutine + `io.Copy`,任一方向出错即双关。它是"关闭语义简陋但足够 frp 用"的基线——对照用。

## 四、去各项目看什么(阅读指引)

| 仓库(本地路径/待克隆) | 文件 | 看什么 |
|------------------------|------|--------|
| gost-x(test/example/gost-x) | `internal/net/transport.go` | 本篇已分析;再看 `bufpool` 的实现(sync.Pool 细节) |
| nps(test/example/nps) | `lib/conn/conn.go` | 本篇已分析;对照 `lib/common/pool.go` 的 buffer 池 |
| frp(待克隆) | `pkg/util/net/` | `Join` 的错误处理与关闭顺序 |
| v2ray-core / sing-box(待克隆) | v2ray `common/buf/copy.go`、sing-box 的 relay 相关 | 内部管道怎么背压、关闭怎么通知 |
| Envoy(只读文档/blog) | tcp_proxy 文档 | watermark 背压机制,M6 之后再考虑 |
| quic-go(test 时引入) | `quic.Stream` | `CancelRead` 读侧半关闭 API 形状 |
| ssh(标准库 golang.org/x/crypto/ssh) | channel 机制 | 窗口流控下拷贝怎么写 |

## 五、取其精华 / 去其糟粕

**取(精华)**

1. **gost 的纯函数形状**:`Transport(rw1, rw2)` 两个参数,横切面一概不进签名——rain-net `Bridge.Bridge(src, dst)` 直接采用;
2. **池化 buffer**:gost 的 `bufpool` 思路(建议 32–64KB,benchmark 后定);
3. **首包重放模式**:nps 的 `rb` 参数本身是个好模式(嗅探读走的字节要补回流里),但位置错了——应放在**嗅探分发器返回的 Conn 适配器里**(nps 自己的 pmux `PortConn` 就是这么做的),不放进 Bridge;
4. **Linux 零拷贝白拿**:`io.Copy` 对 TCP↔TCP 自动走 splice,选 `io.CopyBuffer` 而非手写循环即可享受;
5. **ssh / Envoy 作为远期参考**:窗口流控与 watermark 背压,M6(产品化)再回头看。

**舍(糟粕)**

1. **参数膨胀**:nps 的 8 参签名——横切面(限速/统计/加密)全部改用 `rainnet.Conn` 装饰器,包在传入的 Conn 外面,Bridge 不感知;
2. **方向标志**:`isServer` 这类上下文参数说明职责放错了层,包装器应自己知道自己包的是哪侧;
3. **"一断全断"的关闭语义**:gost/frp 的 Transport/Join 任一方向结束就收工,会把"客户端半关闭请求、等服务端完整响应"的协议掐断——rain-net 用半关闭传播契约替代(design.md 矩阵:CloseWrite 转发、EOF 排空在途数据再关、错误立即双关);
4. **死代码内联**(nps 函数体里注释掉的旧实现)——纯工程习惯问题,记录为提醒。

## 六、rain-net Bridge 的定位(一句话)

**形状抄 gost(纯函数 + 池化 buffer + goroutine 对),关闭语义用自己的(半关闭传播矩阵,收编 star 已有的排空逻辑),横切面走 Conn 装饰器(记账/限速包在 Conn 外)。**

## 七、遗留问题(M5 实现前回答)

- 半关闭传播的排空 buffer 上限(防止对端不关连接导致排空无限等):加可配置 deadline;
- 两条 Conn 能力不对等时(一端支持 CloseWrite 一端不支持)的矩阵补全:目前契约只写了"目的侧支持/不支持",要补"源侧不支持但目的侧支持"的列;
- 流量记账接口:inBytes/outBytes 记在 Conn 装饰器还是 Bridge 返回值(admin API 现在读 streamtable 的计数,迁移时要对齐)。
