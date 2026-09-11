# 项目定位与路线图

> 2026-09-10 整理。本文档记录项目定位、核心架构决策与开发路线,供后续开发与会话参考。
> 协议细节见 `docs/star-protocol.md`,管理 API 见 `docs/admin-api.md`。

## 一、项目定位

rain-net 是一个以学习为目的、最终收敛到成熟产品形态的网络工具,三个核心目标:

1. **便捷启动多协议服务**:http、websocket、http2、QUIC、dns 等协议服务均可通过配置快速拉起;
2. **服务间传输协议转换**(主轴,见下节):server 与 server 之间联通,运输层(tcp/tls/ws/h2/quic/kcp)互为载体,应用层语义原样透传;
3. **插件机制**:沿用 Caddy/CoreDNS 的链式插件模型(`pluginer/` 已实现),便捷扩展自定义需求。

开发过程以学习为导向:每个协议/传输都走一遍"读 RFC → 实现 → 压测 → 写总结文档",文档积累本身是项目最重要的产出之一。

### 明确的边界

- **协议转换指运输层转换**,不是应用层语义转换(不做 DoH 之类的语义映射)。
- 产品主轴是"协议转换网关"。star 隧道(内网穿透,frp 类功能)**已完成、保持可用即可**,退居为一个 egress/插件类型,不再往 frp 方向加功能(补上多路复用流控后封盘)。
- 范围控制:Caddy + gost + frp 三合一的范围对个人项目太大,聚焦协议转换网关一条主线。

## 二、核心架构决策:协议 × 传输正交分层

把系统拆成两个独立维度:

- **协议层**(handler 插件链):socks5、httpProxy、dns、star 隧道——定义字节流语义;
- **传输层**(transport 插件):tcp、tls、ws、h2、quic、kcp——只负责把字节流从 A 搬到 B。

Transport 插件只需实现两个能力,统一适配成 `net.Conn` 形状:

```go
type Transport interface {
    Listen(addr string) (net.Listener, error) // 入站:accept 出标准 net.Conn
    Dial(addr string) (net.Conn, error)       // 出站:拨号返回标准 net.Conn
}
```

"协议转换"在这个模型里不是特殊功能,而是自然结果:listener 用传输 A 接入,bridger 拿到 `net.Conn` 后用传输 B 拨号,双向 `io.Copy`。任意入站传输 × 任意出站传输的组合矩阵免费获得;服务端 ↔ 服务端联通 = 出站 transport 指向另一个 rain-net 节点。

配置模型已具备雏形:`listenerList` 的 `transport` 字段即此设计(旁注 `# transport: kcp、http2`),把它正式化为 transport 插件类型即可。`bridgerList`/`service_bridger.go` 已预留,当前未实现,是下一步主轴。

## 三、关键设计难点

### 1. 半关闭语义不对齐(最重要的坑)

内部流以 **tcp 语义为基准**(关写不关读)。各传输情况:

| 传输 | 半关闭支持 | 说明 |
|------|-----------|------|
| tcp/tls | 支持 | 基准语义 |
| quic stream | 支持 | 基本对齐 |
| http2 stream | 支持(END_STREAM) | 基本对齐 |
| websocket | **不支持** | 关闭帧是整条连接级,无法表达"我这半边完了" |

star 协议 ctrl 通道已处理过此问题(排空在途数据再关外部连接,半关闭),经验要升级为全项目统一约定:不支持半关闭的传输(如 ws)要么降级为整条关闭,要么在帧层补最小关闭标记。决策必须写进文档,每个 transport 实现遵守同一约定。

### 2. 多路复用传输的"连接"定义

h2、quic、kcp(以及自研 star)一条物理连接承载多条流。Transport 的 `Dial` 返回"共享物理连接里的一条流"还是"独占物理连接"?**决策:连接池复用**——Dial 从池里取/建物理连接、开新流,参考 v2ray mux / gost relay。这是同类工具性能差异的关键点。

### 3. 先不做 PacketConn 轴

原始 UDP 数据报是 `net.PacketConn` 形状(无连接),与流式转换模型不合。第一版只做流式传输,QUIC 以 quic-go 的 stream 形态接入;datagram 留作远期。

## 四、里程碑路线图

按依赖顺序,每完成一个传输即压测(吞吐/延迟)并写总结文档:

1. **M1 定型核心抽象**:定义 `Transport` 接口;把现有 tcp/tls 改造成第一批 transport 插件;修复 `internal/star/plugin/forward` 与 `internal/star/zplugin` 的编译错误(未使用 import);半关闭语义约定写入文档。**模型与结构定稿见 `docs/design.md`**(含重构执行顺序 R0–R4,本文里程碑与 R 步骤合并推进)。
2. **M2 websocket transport**:已有 `protocol/ws` 基础;注意它是消息帧协议,需定义帧→流的映射(binary 帧、帧大小上限、ping/pong 保活)。
3. **M3 http2 h2c transport**:学习 HPACK、流控、多路复用。
4. **M4 QUIC transport**(学习价值最高):UDP、TLS1.3 内嵌握手、流状态机,基于 quic-go。
5. **M5 实现 bridger**:入站 × 出站传输自由组合 + 节点间转发。此步完成,产品的独特卖点成立。
6. **M6 产品化收尾**:star 协议按流流控/背压、配置热更新、可观测性、单二进制分发、文档站。
7. 加餐(可选):kcp transport(ARQ 与拥塞控制,独立知识块)。

## 五、现状盘点(2026-09-10)

- 核心测试全绿:`protocol/star`、`internal/star/starserver`(含 e2e);
- 已有:tcp/tls 传输、http/socks5 正向代理、dns server(插件化)、ws codec、star 隧道(握手认证、流注册、半关闭排空、round-robin + 故障转移)、admin API + 内嵌 Web 管理台;
- 已知问题:`plugin/forward`、`zplugin` 编译失败(未使用 import,重构中间态);`cmd/` 下大量实验代码与主工程混放;DNS/pluginer 无测试覆盖;star 协议无按流流控;
- 参考项目:sing-box/v2ray(inbound/outbound 抽象、mux)、Caddy(插件机制、配置模型)、gost(多协议 relay 链)、Envoy(listener/filter/cluster 分层)。
