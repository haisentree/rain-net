# v2ray-core(v2fly v5)调研:插件化代理内核的完整样本

> 2026-10-13。对象:`third_party/v2ray-core`(v2fly/v2ray-core v5.53.0,commit 3861a919,MIT)。
> 回答四个问题:整体架构怎么串、数据面怎么背压/关闭、传输与协议怎么正交、对 rain-net 取什么舍什么。
> 配合 `docs/core-abstraction.md`(七问对照)、`docs/research/bridge.md`(背压/关闭待办)、`docs/references.md` §Xray/v2ray 阅读。
> 下文相对路径均以 v2ray-core 仓库为根。行号采集于 v5.53.0(commit 3861a919),**已在 v5.54.2(e29ac2e6,阅读 fork `~/Project/Git-temp/v2ray-core`)逐一复核一致**——两次上游提交仅涉 quic,链路模型无变化;源码阅读以 fork 最新 master 为基线,路线见 §九。

## 一、基本情况与定位

| 项 | 内容 |
|----|------|
| 上游 | v2fly/v2ray-core v5(注意:**不是** XTLS/Xray-core) |
| 版本 | 5.53.0,Go 1.25.5/toolchain 1.26.1,独立 .git 保留 |
| 许可 | **MIT**——与 EasyTier(LGPL-3.0)不同,读、引、甚至移植代码均可,保留版权声明即可 |
| 规模 | ~1296 个 .go 文件;app/ common/ features/ infra/ main/ proxy/ transport/ 七大目录 |
| 与主线关系 | 纯参考克隆:go.mod 无 replace,无任何 import,物理隔离(同 E-track 纪律) |

两个易混点:

1. **v2fly v5 没有 splithttp/xhttp,也没有 REALITY**——那是 Xray 的演进;references.md 里"看 xhttp 变体"需另克隆 Xray-core。
2. v2fly 在主线之外带了自己的实验传输:hysteria2、dtls、tlsmirror、rrpit(UDP 包转流)、request(伪装流),在 `transport/internet/` 下可见。

## 二、总体架构:Feature 插件框架 + 一条连接的流转

### 2.1 组装机制:三张注册表 + 反射式依赖注入

- **Feature 最小接口**(`features/feature.go:9`):`Type() + Start() + Close()` 三方法即构成可插拔组件。`Type()` 返回接口类型的 nil 指针作注册键,如 `inbound.ManagerType()` 返回 `(*inbound.Manager)(nil)`。
- **运行时对象表**:`common.RegisterConfig(proto配置类型, creator)` → `common.CreateObject(ctx, config)`(`common/type.go:16-35`),reflect.Type → 构造函数的全局 map,各包 `init()` 自注册,`main/distro/all/all.go` 集中 blank import 激活。
- **依赖注入**:`core.RequireFeatures(ctx, func(dep1, dep2 ...))`(`v2ray.go:304`)用反射解析参数类型,依赖齐了立即调用,否则挂到 featureResolutions,之后每次 `AddFeature` 重试(`v2ray.go:327`)。app 内部普遍这样拿依赖,如 `app/dispatcher/default.go:101`。
- **配置别名表**:`common/registry`,proto 消息经 `protoext.message_opt` 注解声明 `type`(类别:"transport"/"security"/"inbound"/"outbound")与 `short_name`(别名,如 ws/freedom),JSON 配置按别名加载(`common/registry/registry.go:47`)。

`Instance` 本体在 `v2ray.go:94`:`features []Feature` + `featureResolutions` + `running`。启动链 `New() → initInstanceWithConfig`(`v2ray.go:181`):遍历 config.App 里的 `google.protobuf.Any` → 查表构造 → `AddFeature`;缺省 feature 用内置兜底(dns/policy/routing/stats)。**`AddFeature` 在 running 状态会直接 `Start()`(`v2ray.go:330`)——运行时热加 handler 是框架原生能力**,与 R8-2b 配置热更新是同一问题的两种解法。

### 2.2 一条 TCP 入站连接的完整流转

```
tcpWorker.callback (app/proxyman/inbound/worker.go:59)
  └─ 注入 session.Inbound{Source,Gateway,Tag} + session.Content 到 ctx
  └─ proxy.Inbound.Process(ctx, TCP, conn, dispatcher)      ← 协议解析(socks/http/vmess…)
       └─ dispatcher.Dispatch(ctx, dest)                     (app/dispatcher/default.go:203)
            ├─ 造两根 pipe 交叉拼成 inboundLink/outboundLink(transport.Link{Reader,Writer})
            ├─ 可选 sniffing:cachedReader 缓存首包嗅探协议/域名后改写目的
            └─ routedDispatch(default.go:300)
                 └─ router.PickRoute → route.GetOutboundTag → outbound.Manager.GetHandler(tag)
                      └─ handler.Dispatch(ctx, link)(app/proxyman/outbound/handler.go:153)
                           ├─ DNS 解析、按 mux 配置决定走 mux 或直连
                           └─ proxy.Outbound.Process(ctx, link, dialer)
                                └─ dialer.Dial(ctx, dest) → internet.Dial 按 streamSettings 出网
```

**协议实现只面对两个窄接口**(`proxy/proxy.go:22-35`):

```go
type Inbound interface {
    Process(ctx, network net.Network, conn internet.Connection, dispatcher routing.Dispatcher) error
}
type Outbound interface {
    Process(ctx, link *transport.Link, dialer internet.Dialer) error
}
```

Handler 壳(连接管理:worker/listen/session 注入/拨号/mux)与 Proxy 协议体(vmess/trojan/…)分离;`transport.Link` 只有 `{Reader buf.Reader; Writer buf.Writer}` 两个字段(`transport/link.go:6`)。

### 2.3 元数据全走 context

`common/session`:`Inbound{Source,Gateway,Tag,User}`、`Outbound{Target,Gateway,Resolver}`、`Content{Protocol(嗅探结果),Attributes map,SniffingRequest}`。写入点→读取点链:worker 写 Inbound/Content → dispatcher 写 Outbound → 路由经 `routing_session.AsRoutingContext(ctx)` 适配成只读 `routing.Context`(11 个只读方法)→ 出站 proxy 读 Target。**全链路只 import session 包,各层互不依赖**——rain-net `connectTable` 之外的另一种横切元数据携带方案。

### 2.4 路由模型

`app/router`:规则顺序遍历、首条命中即选(`router.go:76`);一条规则各条件 AND 成 ConditionChan(domain 用 strmatcher MPH 索引,支持 plain/regex/rootDomain/full 四种;另有 geoip/port/network/user/inboundTag/protocol/Starlark 脚本属性匹配)。输出静态 tag 或经 Balancer(leastping/leastload/random)动态选。`IpIfNonMatch` 策略借 dns.Client 解析后重跑规则。

## 三、数据面:背压在管道里,不在拷贝里

这是 `docs/research/bridge.md` §四"内部管道怎么背压、关闭怎么通知"的答案。

### 3.1 buf 层(common/buf)

- `Buffer` = 2KB 内存 + start/end 双游标(`buffer.go:28`);三种所有权(managed 池化/unmanaged 包装外部/bytespool 按需),`Release()` 按所有权归还。
- 池化:`common/bytespool` 四个 sync.Pool 按 2048×4ⁱ 分桶 = **2K/8K/32K/128K**。
- `MultiBuffer []*Buffer` 是整条数据面的传输单元,关键操作全是**零拷贝指针搬移**(MergeMulti/SplitBytes/SplitSize/Compact);所有权单向转移:**Writer 收到 MultiBuffer 即负责 Release**(`buf/io.go:26`)。
- 批量 IO 摊薄 syscall:读侧 `ReadVReader` 自适应扩批,从 1 个 Buffer 起步读满 ×4、上限 32 个(64KB/readv,`readv_reader.go:14`);写侧把 MultiBuffer 拼成 `net.Buffers` 一次 gather-write。UDP 逐包写。

### 3.2 背压:pipe 有界缓冲 + 阻塞写(transport/pipe/impl.go)

`buf.Copy` 只是"读一批→写一批"的无状态同步循环(`copy.go:79`),**所有缓冲与背压集中在 pipe**:

- 一条 pipe = Mutex + MultiBuffer + 两个容量-1 Notifier + done;`WithSizeLimit(n)` 设界。
- 写满 → 阻塞在 `writeSignal/done` 的 select 上,读者排空并 Signal 后唤醒——背压**同步传导**回上游 socket 接收窗口,中间没有任何异步队列。
- 细节:发生整块合并时返回"慢下来"信号,写者 `runtime.Gosched()` 让出后仍算写成功(半背压);UDP 场景可配 `DiscardOverflow()` 满则丢。
- 读写唤醒用**容量-1 可合并的 Notifier**(`common/signal/notifier.go`:Signal 非阻塞,多次合并为一次),避免 goroutine 堆积。

### 3.3 关闭:两级语义 + done 广播

- **`Close()`(优雅半关)**:写端此后 `io.ErrClosedPipe`,读端**排空存量后**得 `io.EOF`。
- **`Interrupt()`(破坏性全断)**:释放存量数据,读写双方立即 `io.ErrClosedPipe`(`common/interfaces.go` 的 `common.Close/common.Interrupt` duck-type 统一调度,Interrupt 优先)。
- 广播原语 `signal/done`:一次性 `close(chan)`,幂等,所有等待者同时解锁。
- 空闲治理统一为"ActivityTimer 喂狗 → `CancelAfterInactivity` cancel ctx → task.Run 收尾"(`common/signal/timer.go`),每条连接不设 tick goroutine。

### 3.4 dispatcher 的桥接与两端点的双向 copy

`getLink`(`app/dispatcher/default.go:135`)用两根 pipe 交叉拼出双向链路;**真正的双向 copy 发生在两个端点**(如 freedom `freedom.go:146`):

```go
requestDone  = buf.Copy(link.Reader → conn)   // 上行
responseDone = buf.Copy(conn → link.Writer)   // 下行
task.Run(ctx, requestDone, task.OnSuccess(responseDone, task.Close(output)))
```

上行正常结束后先关 `link.Writer`(对端下行读立即 EOF)再等下行排空——半关闭语义在这里落地。出站 handler 收尾约定:Process 失败 `Interrupt(link.Writer)`、成功 `Close(link.Writer)`,最后必 `Interrupt(link.Reader)`(`app/proxyman/outbound/handler.go:173`)。

### 3.5 顺带:common/drain 不是关闭通知

drain(`common/drain/drainer.go`)是防主动探测组件:vmess/shadowsocks 入站鉴权失败时按随机长度继续拖尾读取再关闭,使错误连接的流量形态与正常连接不可区分。读代码时容易误当关闭机制。

## 四、mux 连接池:roadmap "参考 v2ray mux" 的实体

`common/mux` 是自研 **mux.cool** 协议,与 `xtaci/smux` 无关(smux 只在 rrpit 传输里用)。

- **连接池**(`client.go:86`):worker 数组线性找第一个非满的,命中后与队尾 swap(保持"最可能不满的在末尾");没有就 cleanup 死 worker 并新建物理连接;30s 周期清理惰性启动。
- **单条物理连接** = 一对 64KB 有界 pipe + 每流一个上行 goroutine(首包 100ms 超时,等不到也先发"纯头帧"立即建流)+ **单线程下行解帧循环**(`fetchOutput`)。
- **帧格式三件套**:长度前缀帧头(2B metaLen,≤512)、首帧带目标地址(network+port+address)、End/Error 帧显式关流;流式每帧 payload ≤8KB(`SplitSize`);写侧 meta 与 payload 零拷贝指针拼接后一次写物理链路。
- **没有 per-session 流控窗口**——全部背压来自共享的 64KB pipe,任一 session 写满即阻塞所有 session。**队头阻塞是已知取舍**,这是 v2ray mux 公认短板;自研若要公平性需补 per-session credit,建议在 rain-net mux 设计时就写进决策记录。
- 对端不存在的 session:回发 End 帧并丢弃数据;物理连接空闲由 monitor 每 16s 检查,0 session 即整体关闭。

## 五、传输正交性:一个签名 + 三张表,零 switch-case

v2fly v5 没有 Xray 的 `Transporter` 接口,传输抽象 = 统一拨号签名 + 三张注册表:

1. **协议名 → 拨号/监听函数**:`internet.RegisterTransportDialer(protocol, Dial)`(`transport/internet/dialer.go:26`),签名 `dialFunc(ctx, dest, *MemoryStreamConfig) (Connection, error)`;监听侧对称(`tcp_hub.go:11`)。
2. **proto 类型 → Handler creator**:即 §2.1 的 common.RegisterConfig。
3. **配置别名 → proto 消息**:即 §2.1 的 registry,proto 注解声明 short_name(ws/websocket 同一 Config 靠 `transport_original_name` option 重定向)。

**组合声明在 StreamConfig**(`transport/internet/config.proto:32`):

```
StreamConfig { protocol_name; transport_settings(Any); security_type(字符串,安全层 proto 全名);
               security_settings(Any); socket_settings }
```

传输 × 安全层 × Socket 参数三个维度全是声明式组合。TLS 不是各传输硬编码,而是独立可插拔接口 `security.Engine`(`security/security.go:9`,`Client(conn, opts...) (Conn, error)`),utls 是另一实现;传输特化逻辑(如 h2 对 ALPN 强校验 h2、websocket 需 NetDialTLSContext 挂 ALPN)留在传输包内。

**正交性的证明**:vmess 出站只拿 `dialer.Dial(ctx, rec.Destination())` 返回的 `net.Conn`(`proxy/vmess/outbound/outbound.go:63`),底层裸 TCP 还是 ws+TLS 完全透明。每个新传输只需"init() 注册三件套 + blank import",配置层与运行时自动接上。

**mkcp 是"包→流网关"的范本**(`transport/internet/kcp`):伪装头部 PacketHeader(srtp/utp/wechat/wireguard 可选)+ AEAD 加密层(Seed 用 AES-GCM,否则 legacy FNV+xor)+ 自研 KCP 段协议(非 kcp-go),三层叠加后 `NewConnection` 输出标准 net.Conn,此后与 TCP 传输完全同构。

**proxy 家族速览**:freedom(直连出站)、blackhole(写一段 canned response 后休眠+Interrupt)、socks/http/dokodemo(入站,解析目标后 `dispatcher.Dispatch` 双拷贝)、vmess(AEAD/legacy 分支在组装 Option 时定)、vless(明文头、无内建加密,依赖外层 TLS)。

## 六、对 rain-net 的启示

### 6.1 design.md 决策的代码验证

design.md §二"通用语言必须是扩展 net.Conn(v2ray 反例)"——代码证实代价:`transport.Link` 不是 `net.Conn`,所以 `http.Server` 这类现成资产无法直接挂载,v2ray 自己的 HTTP 入站只能手写 `handlePlainHTTP`(`proxy/http/server.go`)。core-abstraction.md 七问里 v2ray 的答案全部落地可查:

| 七问 | v2ray 答案(可直读) |
|------|---------------------|
| 组件间交换什么 | `transport.Link{buf.Reader, buf.Writer}`(自研,非 net.Conn) |
| 传输是否独立维度 | 是:`StreamConfig.protocol_name` + 三张注册表 |
| 多路复用 Dial 返回什么 | 池化物理连接上的逻辑流,池私有(`common/mux/client.go`) |
| 元数据与路由怎么携带 | 随连接的结构体(context 值传递):`session.Content` |

### 6.2 取其精华(MIT,可移植)

1. **pipe 的背压/两级关闭语义**:有界缓冲 + 阻塞写 + Close/Interrupt 分离 + done 广播——rain-net Bridge 的关闭传播矩阵可直接对照实现。
2. **"拷贝任务 + 空闲超时"公共骨架**:`task.Run` + `CancelAfterInactivity` + `buf.UpdateActivity` 贯穿所有 handler,值得抽成 rain-net 的公共 Bridge 骨架。
3. **readv 自适应批量读 + gather 写**:同构的摊薄 syscall 手法。
4. **mkcp 三层叠加结构**(伪装头 + AEAD + 可靠流)——M7(kcp)的直接参照。
5. **首帧建流技巧**:mux 建流不等真实首包,100ms 超时先发纯头帧,降低交互延迟。

### 6.3 去其糟粕 / 别照搬

1. **mux 无 per-session 流控**:队头阻塞,自研需 per-session credit。
2. **自研 Link 通用语言**:生态资产挂不上的代价已验证,rain-net 坚持 net.Conn 包装式。
3. **反射式 RequireFeatures + 三张全局注册表**:灵活但隐晦(注册靠 blank import 的副作用),rain-net 的显式装配更可控——取其"配置驱动组装"的思路即可,不必抄机制。

### 6.4 对 R 线各任务的直接对照物

| rain-net 任务 | v2ray 对照物 |
|---------------|--------------|
| M3/R5 h2 transport | `transport/internet/http`(golang.org/x/net/http2 直用,ALPN 强校验) |
| M7/R6 kcp / quic | `transport/internet/kcp`(三层叠加)、`transport/internet/quic`(quic-go Transport 拨号) |
| mux/连接池(roadmap §四) | `common/mux/client.go` 全套 |
| Bridge 背压/关闭(bridge.md §四) | `transport/pipe/impl.go` + `common/buf/copy.go` + `common/signal/done` |
| DNS(R8-3) | `app/dns` + `features/dns` 接口层 |
| 配置热更新(R8-2b) | `v2ray.go:330` AddFeature 运行中热加 handler |

## 七、遗留问题

- sing-box 仍未克隆:bridge.md §四"v2ray / sing-box 对照"只完成一半;sing-box 的 relay 与 v2ray-transport 配置模型是另一半。
- Xray-core 待克隆:xhttp 变体与 REALITY(v2fly v5 均无)。
- `app/dns` 的分流与缓存策略未深读(R8-3 刚迁移完 DNS,值得回头对照)。
- `common/strmatcher` 的 MPH 索引实现未深读(路由规则量大时的性能来源)。

## 八、链路设计模型(节点级汇总)

> 会话内交互模型(v2ray-link-model)的文本存档。三部分:全链路图 + 节点卡片 + 数据面拓扑。
> 背压/关闭语义的完整分析见 §三,组装机制见 §五,此处只留模型骨架。

### 8.1 连接生命周期(实线=数据流,虚线=控制/可选)

```
              ────────────────────── 数据流 ──────────────────────▶
用户 ─▶ ① 入站worker ─▶ ② 入站协议层 ─▶ ③ dispatcher ─▶ ⑤ 出站handler ─▶ ⑦ 出站协议层 ─▶ ⑧ 传输拨号 ─▶ 远端
        accept·ctx注入    解析出dest      pipe交叉·嗅探     DNS·mux判断      双向buf.Copy     传输×安全×socket
                                          │                    │
                                          ├─▶ ④ 路由(控制流)     ├─▶ ⑥ mux池(可选)
                                          │  PickRoute→tag      │  64KB pipe·session复用
                                          ◀─┘ tag 回注 dispatcher ◀─┘ 建流后仍经 handler.Dial 拨物理连接
```

### 8.2 节点卡片(职责 / 接口 / 源码 / 要点)

**① 入站 worker** — accept 用户连接,组装本次请求的元数据 ctx
- 接口:`tcpWorker.callback(conn internet.Connection)`;源码:`app/proxyman/inbound/worker.go:59-109`、`always.go:44`
- 要点:`session.ContextWithInbound` 注入 {Source, Gateway, Tag},Tag 是路由 inboundTag 规则的来源;handler 是壳——worker 管监听与端口,协议实现在 proxy

**② 入站协议层** — 解析协议头、认证,得出目标 dest,此后不再感知 socket
- 接口:`Process(ctx, network, conn, dispatcher routing.Dispatcher) error`(`proxy/proxy.go:23`);源码:`proxy/socks/server.go:101`、`proxy/vmess/inbound/inbound.go:232`
- 要点:解析完即 `dispatcher.Dispatch(ctx,dest)` 拿双向 Link,之后只是 buf.Copy 泵;drain 防主动探测——鉴权失败按随机长度拖尾读取再关闭;新协议只需 Process + init() 注册

**③ dispatcher** — 造两根交叉 pipe 组成双向 Link;嗅探;按路由结果转交出站
- 接口:`Dispatch(ctx, dest net.Destination) (*transport.Link, error)`;源码:`app/dispatcher/default.go:135`(getLink)、`:203`(Dispatch)、`:300`(routedDispatch)
- 要点:cachedReader 缓存首包(200ms 超时)嗅探协议/域名,可改写目的;流量统计以 SizeStatWriter 装饰器插在 pipe 链,业务无感

**④ 路由** — 规则顺序匹配,首条命中即选,输出出站 tag
- 接口:`PickRoute(ctx routing.Context) (Route, error)`(`features/routing/router.go:11`);源码:`app/router/router.go:76-131`、`condition.go`
- 要点:条件 AND 组合;domain 走 strmatcher(MPH 索引);Balancer leastping/leastload/random 按 tag 前缀筛候选;IpIfNonMatch 借 DNS 解析后重跑;元数据经只读 routing.Context 适配,不向实现层泄漏

**⑤ 出站 handler** — DNS 解析、mux/直连决策,兼任 internet.Dialer
- 接口:`Dispatch(ctx, link)` / `Dial(ctx, dest) (Connection, error)`;源码:`app/proxyman/outbound/handler.go:153-185`、`:196-278`
- 要点:链式代理 = 经 pipe 串到另一 tag 的 handler;收尾约定:失败 `Interrupt(link.Writer)`、成功 `Close`,最后必 `Interrupt(link.Reader)`

**⑥ mux 池(可选)** — 多条逻辑 session 复用一条物理连接(mux.cool,非 smux)
- 接口:`ClientManager.Dispatch → IncrementalWorkerPicker → ClientWorker`;源码:`common/mux/client.go:86-124`(池)、`:148`(64KB pipe)、`session.go`
- 要点:线性找第一个非满 worker + 队尾 swap,无则新建,30s 惰性清理;单连接 = 一对 64KB 有界 pipe + 每流一个上行 goroutine + 单线程下行解帧;无 per-session 流控→队头阻塞是已知取舍,自研要公平需补 per-session credit

**⑦ 出站协议层** — 在 Link 与真实连接之间做协议编码与双向泵
- 接口:`Process(ctx, link, dialer internet.Dialer) error`(`proxy/proxy.go:32`);源码:`proxy/freedom/freedom.go:95-184`、`vmess/outbound/outbound.go:63`
- 要点:从 ctx 取 `session.Outbound.Target` → `dialer.Dial`(重试 5 次);`task.Run(上行, 下行)` 并行,上行完成先关 link.Writer(对端 EOF)再等下行排空——半关闭语义;协议只面对 Dialer,底层传输完全透明

**⑧ 传输拨号** — 按 streamSettings 组合 传输 × 安全层 × Socket,返回 net.Conn
- 接口:`dialFunc(ctx, dest, *MemoryStreamConfig) (Connection, error)`;源码:`transport/internet/dialer.go:35-67`、`config.proto:32-48`(StreamConfig)
- 要点:protocol_name 查传输注册表;security_type 经 security.Engine 包装 TLS/utls;Connection = net.Conn 别名,出站再包一层统计

### 8.3 数据面拓扑(dispatcher 的"交叉"实质)

```go
// app/dispatcher/default.go:135 getLink
inboundLink  = Link{Reader: downlinkReader, Writer: uplinkWriter}   // 给入站 proxy:读下行、写上行
outboundLink = Link{Reader: uplinkReader,   Writer: downlinkWriter} // 给出站 proxy:读上行、写下行
```

```
用户 conn ──上行 buf.Copy──▶ uplink pipe(有界 64KB~512KB) ──▶ 远端 conn
用户 conn ◀──下行 buf.Copy── downlink pipe(远端→用户)      ◀── 远端 conn
```

- 背压:pipe 写满 → `WriteMultiBuffer` 阻塞,同步传导回 socket;copy 循环本身无队列
- 关闭:`Close` 半关(读侧排空后 EOF)/ `Interrupt` 全断(立即双向 `io.ErrClosedPipe`);广播用 `signal/done`
- 批量 IO:读侧 readv 自适应 1→32×2K,写侧 `net.Buffers` gather;MultiBuffer 所有权单向转移

### 8.4 组装模型一句话

协议层(vmess/vless/trojan/socks/http/freedom/dokodemo/blackhole…) × 安全层(none/tls/utls/dtls/tlsmirror) × 传输层(tcp/ws/h2/grpc/quic/kcp/httpupgrade) 全部由 StreamConfig 声明式组合;机制 = 三张注册表(传输:`protocol_name→dialFunc` dialer.go:26;对象:`proto Type→creator` type.go:16;别名:`注解→proto 消息` registry.go:47);激活 = `main/distro/all/all.go` blank import。

## 九、源码阅读路线(基线:fork master,当前 v5.54.2)

> 2026-10-14 立项。fork 在 `~/Project/Git-temp/v2ray-core`(origin: haisentree/v2ray-core)——读、断点、改实验都在 fork;**笔记一律回填本文档**,不散落第二个仓库。
> §八 的 8 个节点即 8 个阅读站点,行号已按 v5.54.2 校准。

### 9.1 环境搭建(一次性)

```bash
cd ~/Project/Git-temp/v2ray-core
git checkout -b read-v5.54.2                                     # 阅读分支,避免和上游同步互相干扰
git remote add upstream https://github.com/v2fly/v2ray-core.git   # 以后追上游用
```

编译与运行(已在 v5.54.2 验证):

```bash
cd ~/Project/Git-temp/v2ray-core
go build -o /tmp/v2ray-core/v2ray ./main          # 首次约 2 分钟(依赖经 goproxy.cn)
/tmp/v2ray-core/v2ray version                     # V2Ray 5.54.2
/tmp/v2ray-core/v2ray test -c /tmp/v2ray-core/config.json   # 配置校验: Configuration OK.
/tmp/v2ray-core/v2ray run  -c /tmp/v2ray-core/config.json   # 前台运行,Ctrl-C 停
```

- 最小配置 = socks 入站(`127.0.0.1:10808`,tag socks-in)+ freedom 直连出站(tag direct);把 loglevel 改 `"debug"` 可看每条连接的内部流转日志。
- 造流量:`curl --socks5 127.0.0.1:10808 http://127.0.0.1:18080/`(本地起任意 HTTP 服务即可)。
- 断点调试:`dlv exec /tmp/v2ray-core/v2ray -- run -c /tmp/v2ray-core/config.json`。

### 9.2 三遍读法

**第一遍:组装骨架(半天,约 500 行)**——目标:搞懂配置里的 `protocol: socks` 怎么找到构造函数。按 `main()` 执行顺序:

1. `main/main.go`(40 行,入口,三个子命令);
2. `main/distro/all/all.go`——blank import 清单,这个二进制里有什么全在这;
3. `common/type.go`(70 行,RegisterConfig/CreateObject,注册机制之根;断点 :28 看构造顺序);
4. `v2ray.go`(Instance、AddFeature :327、RequireFeatures 反射式 DI);
5. `infra/conf/v4/v2ray.go` 的 `Config.Build()`(JSON → proto Any)。

**第二遍:一条请求的一生(1–2 天,核心)**——沿 §八 8 站,每站"读一个文件 + 断点验证一次":

| 站 | 文件:行 | 断点看什么 |
|----|---------|-----------|
| ① | `app/proxyman/inbound/worker.go:59` | session.Inbound 注入后的 ctx |
| ② | `proxy/socks/server.go:101`(Process 入口 :65) | dest 怎么从 SOCKS5 握手里解析出来 |
| ③ | `app/dispatcher/default.go:135/203` | **全项目最重要的文件**:两根 pipe 怎么交叉成 Link |
| ④ | `transport/pipe/impl.go` | 写满阻塞那几行(errBufferFull,背压实体) |
| ⑤ | `app/proxyman/outbound/handler.go:153/196` | DNS 解析、mux/直连分岔 |
| ⑥ | `proxy/freedom/freedom.go:95` | task.Run 双向泵、半关闭顺序 |
| ⑦ | `transport/internet/dialer.go:35` | protocol_name 查表 → 真实拨号 |
| ⑧ | `common/buf/copy.go:79` | 拷贝循环本体(约 30 行) |

产出:一张实际调用栈时序图,回填 §八。

**第三遍:垂直深入(每模块 1–2 天,按 rain-net 兴趣排序)**:

1. 数据面:`common/buf/{io,multi_buffer,readv_reader}.go` + `transport/pipe/`——读完回填 bridge.md 的"取其精华";
2. mux:`common/mux/{frame,client,session,server}.go`——自画一遍帧格式;重点验证"无 per-session 流控 → 队头阻塞";
3. 传输垂直切片:先 `websocket`(最薄,注册三件套范本),再 `kcp`(伪装头 + AEAD + KCP 三层叠加)——对应 R4/M7;
4. `app/dns`——对照 R8-3 刚做完的 DNS 迁移;
5. 可选:sniffing(`default.go:30` 的 cachedReader)、`common/drain`(防主动探测)。

### 9.3 跳读清单

`*.pb.go`、`errors.generated.go`、`mocks.go`(生成物);`core.go` 里 200 行 ASCII art;`infra/` 除 `infra/conf/` 外的构建脚手架;`hysteria2/dtls/rrpit` 实验支线;xhttp/REALITY 在 Xray-core,v2fly 没有。

### 9.4 fork 实验清单(改代码验证理解)

1. `transport/pipe` 的 `WithSizeLimit` 改成无界 → 观察背压消失后的内存行为;
2. 照 `proxy/blackhole`(约 30 行,最简 outbound)抄一个极简出站注册进去 → 验证懂了注册机制;
3. mux 帧里加一个自定义 option 字节,两端各改一处,看会话怎么崩 → 理解帧格式容错。

节奏:第一遍 + 第二遍一周内可完成;完成后本文档从"调研地图"升级为"亲自走过的地图"——各站补上实测调用栈与实验现象。

## 十、与 rain-net 链路模型对照

> 2026-10-14。依据 design.md / core-abstraction.md 与 rain-net 实际代码(`pkg/rainnet`、`internal/{transport,bridge,router,decorator}`)。
> 回答:同享"路由选向"骨架的两个模型,为什么长成了不同的样子。

### 10.1 总括与形状对照

**一句话**:最大分叉在组件之间的通用语言——v2ray 选自研 `Link`(buf 流 + 内部管道),为此造出 dispatcher 中枢、pipe 背压、done 广播一整套框架原语;rain-net 选扩展 `net.Conn`,链路退化成直线调用 + 一个哑 Bridge,框架原语薄得多。这是 design.md §二 "v2ray 反例" 决策的模型级验证。

```
v2ray(7 环节 + 中枢 + 内部管道):
worker ─▶ 入站协议 ─▶ dispatcher ─▶ 出站handler ─▶ 出站协议 ─▶ 传输拨号 ─▶ 远端
              │            │ 路由分支↑tag        │ mux 分支↑建流
              └─ socket    两根 pipe 交叉 = transport.Link(数据进框架后脱离 socket)

rain-net(6 角色,直线,无中枢无内部管道):
Transport.Listen ─▶ Ingress ─▶ Handler 链 ─▶ Router ─▶ Dialer ─▶ Bridge(src,dst) ─▶ 目标
   (net.Listener)     └────────── 数据全程是 rainnet.Conn,两端直接持 Conn ──────────┘
```

v2ray 里数据穿过框架时"变形"两次(socket → buf 流 → socket),所以需要 dispatcher 这个中枢持路由、造管道、插装饰器;rain-net 里 Conn 从 accept 到 Bridge 不变形,环节之间是普通函数调用,没有可以"居中"的位置。

### 10.2 节点级映射

| v2ray 环节(§八) | rain-net 对应物 | 关键差异 |
|---|---|---|
| ① worker(监听+注入元数据) | Ingress(Transport.Listen + 装配) | 元数据:ctx 值传递 vs `Meta{}` 随 Conn 字段 |
| ② 入站协议层 | Handler 链(pluginer) | 同构;v2ray 调 Dispatch,rain-net 调 `SetTarget` 后交 Router |
| ③ dispatcher 中枢 | **无对应物** | 被"直线调用 + Bridge"结构性取代 |
| ④ 路由 | Router | v2ray 是规则引擎(条件族/geoip/MPH/balancer),rain-net 是收窄版规则表 `(ingress,identity,target)→dialer`,首中即停一致 |
| ⑤ 出站 handler | Dialer(带凭据的出站) | 两者都兼任"拨号能力"持有者 |
| ⑥ mux 池 | **Transport 实现内部私有池** | v2ray 把 mux 当框架功能(独立协议),rain-net 当传输细节(gost 做法),接口不变 |
| ⑦ 出站协议层 | **并入 Dialer**(star 出站即协议适配) | v2ray 的 ⑦⑤ 对称在 rain-net 体现为自相似:`dialer/star` ↔ 对端 `handler/star`——"隧道只是又一种出站" |
| ⑧ 传输拨号 | Transport.Dial | streamSettings 运行时任意组合 vs 装配期按配置名绑定 |

### 10.3 关键维度差异

**通用语言——分叉的根源。** v2ray 的 `Link{buf.Reader, buf.Writer}` 不是 net.Conn,`http.Server` 挂不上,HTTP 入站只能手写 `handlePlainHTTP`;rain-net 的 `rainnet.Conn = net.Conn + CloseWrite + Meta + SetTarget`,生态资产直接可用。代价反向:v2ray 的 pipe 天然是嗅探/mux/统计的挂点,rain-net 要靠 Conn 装饰器与适配层补——首包嗅探为例,v2ray 的 `cachedReader` 靠 pipe 语义免费拿到"读了再塞回",rain-net 的 `internal/ingress/sniff` 得自己做首包重放(bridge.md 记过的 nps 教训)。

**背压与关闭——同一问题、不同层。** v2ray 在框架层显式建模:pipe 有界缓冲阻塞写、Close/Interrupt 两级关闭、done 广播。rain-net 在语言层解决:`io.Copy` 逐块天然反压,框架不另造(七问之六的决策);半关闭进 `Conn.CloseWrite()` 接口成编译期契约,传播矩阵在 `internal/bridge/bridge.go` 落地——EOF 后 `CloseWrite` 再排空、`DrainTimeout` 防对端挂死、ctx 取消立即双关。对照 v2ray "上行完成先关 link.Writer 再等下行排空",语义等价,rain-net 实现(bridge.go:142-158)更薄。

**元数据——显式换表达力。** rain-net Meta 四字段与 v2ray session 主干一一对应(Listener↔Inbound.Tag、Remote↔Source、Identity↔User、Target↔Outbound.Target),只读 + SetTarget 一次写入,显式可测。但 v2ray `Content.Attributes`(任意键值、forcedOutboundTag、路由属性回写)没有对应物——将来做 DNS 分流这类"路由附带属性"需求时,Meta 是第一个要扩展的点。

**正交性的实现位置。** v2ray 是运行时组合:StreamConfig 里 Any + 字符串键、三张注册表,同一 outbound 可挂不同 streamSettings;rain-net 是装配期组合:`transport.Registry[string]BuildFunc`,配置 transport 字段静态绑定。网关场景(一个 ingress 固定一种传输)够用且简单得多;v2ray 的灵活性是代理产品需求。

### 10.4 结构性结论

1. **分叉点验证了设计**:自研 Link 的全部复杂性(dispatcher、pipe、背压、关闭广播)都是那一个选择的 downstream 代价;rain-net 用 net.Conn 语言把它压缩成一个 160 行纯函数 Bridge。
2. **v2ray 两件东西值得持续盯着**:一是 pipe 两级关闭语义(Close 排空后 EOF / Interrupt 立即断)——starStreamAdapter 做 CloseWrite/Close 区分时(design.md §九.2 的 reserved 位)可直接对照;二是 mux 首帧建流(不等真实首包、100ms 超时发纯头帧)——R6 quic 连接池调优可参考。
3. **路由是刻意收窄**:无条件族、无 geoip、无 DNS 重跑,换来 admin API 动态增删的顺序语义可预测;坚持这个收窄,v2ray 路由引擎的复杂度是它产品形态的产物。
4. **真正该抄的一个细节**:v2ray 出站 handler 的收尾约定(失败 `Interrupt(link.Writer)`、成功 `Close`、最后必 `Interrupt(link.Reader)`)映射到 rain-net 是 Dialer 返回后对 Conn 的关闭责任划分——目前 bridge.Bridge 的 defer 双关已覆盖;但若将来 Dialer 失败要区分"目标不可达(可重试)"与"已建连后失败(须双关)",这个约定值得写进 Dialer 契约。

## 附:运行记录(2026-10-13)

- **编译**:Go 1.26.3,`cd third_party/v2ray-core && go build -o /tmp/v2ray-core/v2ray ./main`,依赖经 goproxy.cn 拉取,一次通过。
- **配置校验**:`./v2ray test -c config.json` → `Configuration OK.`;最小配置 = socks 入站(127.0.0.1:10808,tag `socks-in`)+ freedom 直连出站(tag `direct`)。
- **启动**:`./v2ray run -c config.json` → 日志 `listening TCP on 127.0.0.1:10808` / `V2Ray 5.53.0 started`。
- **链路验证**:本地 `python3 -m http.server 18080` + `curl --socks5 127.0.0.1:10808 http://127.0.0.1:18080/` 取回内容成功;日志完整走了一遍 §二.2 的链路——`app/dispatcher: default route for tcp:127.0.0.1:18080` → `proxy/freedom: opening connection` → `transport/internet/tcp: dialing TCP`。尾部 `connection ends > context canceled` 是连接结束后的 ctx 回收日志,非错误。
- 配置样例与产物在 `/tmp/v2ray-core/`(临时目录,重启即失)。
- 2026-10-14 复核:fork 最新版 v5.54.2(e29ac2e6)重新编译 + socks→freedom 链路复验通过;§八 全部行号在新版逐一核对一致(两次上游提交仅涉 quic),阅读基线切换为 fork master,路线见 §九。
