# 网络抽象模型:调研与 rain-net 设计提案

> 2026-09-10。对应 roadmap M1(定型核心抽象)。前置阅读:`docs/roadmap.md`、`docs/references.md`、`docs/research/gost.md`、`docs/research/nps.md`。
> 本文回答两个问题:业界的网络抽象模型有哪些套路?rain-net 应该选哪种组合?
> **注:本文是调研与论证;模型定稿(含命名规范、目录结构、迁移映射)以 `docs/design.md` 为准,§3.7 三个开放点已在 design.md 定案。**

## 一、业界的四种抽象模型

### 0. 基线:net.Conn / net.Listener(Go 原生)
Go 把一切网络能力归结为"双向字节流 + 监听器"。它建模了连接与字节,但**不建模**:多路复用、元数据(连接的身份/来源/目标)、路由决策、半关闭(`net.Conn` 接口本身没有 `CloseWrite`,只有 `*net.TCPConn` 等具体类型有)、按流的背压。所有项目都在这之上补齐自己缺的那几块——补的方式决定了抽象风格。

### 模型 A:一切皆 Conn/Listener(包装式)
代表:yamux、smux、quic-go(stream 可适配)、kcptun、chisel、nps 桥接。
新能力(多路复用、加密、kcp)全部包装成 `net.Listener`/`net.Conn` 的外形:如 yamux 的 `Session.Open/Accept` 返回 net.Conn。组合方式就是层层包裹。
- ✅ 通用语言红利:任何接受 net.Conn/net.Listener 的既有代码(http.Server、tls、你的 handler 链)原样可用;每个新传输都是独立小模块;
- ❌ 语义不统一:各包装的半关闭/超时/关闭行为不一致;没有元数据与路由的位置;yamux 的 Accept 之外,谁负责连接池没有约定。

### 模型 B:入站/出站 + 路由(inbound/outbound 式)
代表:v2ray/Xray、sing-box。
inbound 解码任意协议 → 框架内部的"链路"(双向流 + 元数据)→ router 按规则选 outbound → outbound 编码到目标。传输(ws/h2/quic)是 inbound/outbound 的子属性(streamSettings)。
- ✅ 协议转换与流量路由是一等公民;同构节点间转发天然支持;
- ❌ 框架接管连接生命周期,组件自主性低;内部链路抽象自研,失去 net.Conn 生态;对本文体量的项目偏重。

### 模型 C:组件注册 + 角色分解(装配式)
代表:gost v3(Service = Listener + Handler,出站 = Dialer + Connector,Transporter 可含 Multiplexer)、Envoy(listener → filter chain,transport_socket 可替换传输)。
每个角色一个最小接口 + 泛型注册表,配置驱动装配。
- ✅ 与"配置文件 + 插件机制"的产品形态天然匹配;接口粒度即扩展点;横切面(限流/认证/观测)可以全部接口化(gost 已验证);
- ❌ 概念数量多,接口动物园风险;各角色职责边界要靠纪律维持(gost 的 Dialer/Connector 分离有人觉得过度设计)。

### 模型 D:中间件链(数据面处理式)
代表:Caddy、CoreDNS,以及 rain-net 已有的 pluginer。
请求/连接按序穿过插件链,每站可改写、可短路。它**不是完整的网络模型**,而是协议语义层的组织方式,必须寄生在 A/B/C 之一的骨架上。

### 关键洞察
四种模型不是互斥选项,成熟产品都是**分层组合**:拿 A 当组件之间的通用语言,拿 C 当装配骨架,拿 D 做数据面协议处理,把 B 的路由思想收窄为一张路由表。rain-net 已经拥有 D(pluginer)和雏形的路由表(connectTable),缺的是把 A、C 补齐并定准边界。

## 二、抽象设计必须回答的十个问题

| # | 问题 | 业界常见答案 |
|---|------|-------------|
| 1 | 数据单元是字节流还是消息? | 流式为主;dns 等消息协议在其上自行分帧 |
| 2 | 组件之间交换什么(通用语言)? | net.Conn(包装式)/ 自研 Link+元数据(v2ray)/ 裸接口参数(gost) |
| 3 | 传输是不是独立维度? | 是:transport_socket(Envoy)、streamSettings(v2ray)、Transporter(gost);rain-net 配置里已有 transport 字段 |
| 4 | 多路复用下 Dial 返回什么? | 池化物理连接上的逻辑流,池由 transport 实现私有(v2ray mux、gost Multiplexer) |
| 5 | 半关闭统一约定放哪? | 通用语言层定义能力接口(见 §3.2),不支持者显式报错 |
| 6 | 背压怎么做? | io.Copy 天然逐块反压;跨多路复用时靠 per-stream 窗口(yamux),框架不另造 |
| 7 | 元数据与路由怎么携带? | 随连接的结构体字段(v2ray Content)或独立路由表(gost hop、rain-net connectTable) |
| 8 | 认证在传输层还是协议层? | 都有、别混:TLS 在传输层,vkey/password 在协议层(star 已如此) |
| 9 | 生命周期谁管? | pluginer 已有 OnStartup/OnShutdown 钩子,沿用 |
| 10 | 横切面挂哪? | 接口化注入(gost 横切面全家桶),v1 可以先挂在 Conn 元数据与 bridger 上 |

## 三、rain-net 设计提案

### 3.1 选型结论

**C 骨架 × A 语言 × D 数据面 × 路由表**:

- 装配骨架用模型 C:五个角色接口(Ingress / Handler / Dailer / Bridger / Transport),延续现有 config 三表(listenerList/handlerList/dailerList)的概念,新增 transport 正交维度;
- 组件通用语言用模型 A:**扩展版 net.Conn**(见下),不发明自研 Link;
- 数据面沿用模型 D:pluginer 插件链原样不动,它就是 Handler 层;
- 转发决策沿用已有 connectTable(B 思想的收窄版),不引入通用规则引擎。

### 3.2 核心接口(v1 草案)

```go
// transport 包:传输层唯一接口(v1 只做流式,不做 PacketConn)
type Transport interface {
    Listen(ctx context.Context, addr string) (net.Listener, error)
    Dial(ctx context.Context, addr string) (net.Conn, error)
}
// 注册:Registry[T],同 gost;tcp/tls 首发实现
// 多路复用传输(h2/quic/star/ws)在实现内部私有化连接池,Dial 返回逻辑流,
// 接口不变——这正是 gost 把 Multiplexer 藏进 Transporter 的做法。

// rainnet 包:组件之间的通用语言
type Conn interface {
    net.Conn
    CloseWrite() error      // 半关闭;不支持返回 ErrNotSupported(见 roadmap 半关闭矩阵)
    Meta() *Meta            // 只读元数据
}

type Meta struct {
    IngressName string     // 从哪个 listener 进来
    RemoteAddr string      // 真实源地址
    Identity    string     // 协议层认证出的身份(如 star clientProxyName)
    Target      string     // 协议层解析出的目标(addr/path/domain)
}
```

配套角色(多数已有对应物,只是没收敛成接口):

| 角色 | 接口形状 | 现有对应物 |
|------|---------|-----------|
| Ingress | `Transport.Listen` + 绑定 handler 链的 Server | `listenerList` + register.go 的 type switch |
| Handler | pluginer 插件(不变) | `handlerList` |
| Dailer | `Dial(ctx, target string) (rainnet.Conn, error)` | `dailerList`、`makeDailers()`(空壳待实现) |
| Bridger | 入站 Conn × 出站 Conn,双向 `io.CopyBuffer` + 半关闭传播 | `bridgerList`/`service_bridger.go`(占位) |
| Transport | 见上 | register.go 里 `transport` 字段的硬编码 switch |

### 3.3 为什么通用语言选扩展 net.Conn(本设计最重要的决策)

1. **生态兼容红利**:yamux/quic-go/tls/star 全部能以低成本适配成 net.Conn;反过来,`http.Server.Serve(l net.Listener)`、`tls.NewListener`、任意第三方 handler 都能直接吃 rain-net 的 listener——"任意协议 × 任意传输"的组合是免费的;
2. **半关闭有处安放**:自己定义 `rainnet.Conn` 才能把 `CloseWrite` 提为接口方法(原生 `net.Conn` 没有),roadmap 的半关闭矩阵由此落地为编译期约定;
3. **元数据最小侵入**:`Meta()` 是只读结构体指针,不用 context 携带,显式可测;
4. **对照代价**:v2ray 式自研 Link 意味着每个第三方协议都要写适配器,gost 的裸接口参数则让 http.Server 这类现成资产无法直接挂载。

### 3.4 连接生命周期全链路(以两个真实场景走查)

**正向代理**(浏览器 → socks5 handler → 直连):
```
tcp transport.Listen → accept 得 net.Conn → 包成 rainnet.Conn(Meta.IngressName)
→ pluginer 链:socks5 插件解析出 Meta.Target 并认证
→ Dailer(直连型) Dial(Meta.Target) → bridger 双向拷贝
```

**反向代理**(外网用户 → proxy listener → star 隧道 → 内网服务):
```
ctrlproxy(tls transport)accept → star 握手/注册流 → hub 流表
proxy listener accept → 查 connectTable(round-robin 选流)
→ star dailer 型 Dailer.Dial:connOpen → 客户端拨内网 → 流适配成 rainnet.Conn
→ bridger 拷贝;半关闭经 streamClose 传播,排空在途数据(star 已实现)
```

### 3.5 从现有代码的迁移步骤(小步,每步可测)

1. 新建 `internal/transport`:接口 + Registry + `tcp`/`tls` 实现;`register.go` 的 transport switch 改为查表(纯重构,行为不变);
2. 新建 `rainnet.Conn` 接口与两个适配器:`netConnAdapter`(普通 conn,CloseWrite 对 *net.TCPConn/tls.Conn 转发)、`starStreamAdapter`(star 流,CloseWrite → streamClose 帧);
3. starserver 内部 dailer.go/streamtable.go 的连接流转改用 rainnet.Conn 签名;
4. `Bridger` 接口占位 + 第一个实现(直连型:两条 Conn 互拷),M5 时再扩展到跨 transport;
5. 修掉 `plugin/forward`、`zplugin` 编译错误(roadmap M1 遗留)。

### 3.6 明确不做(v1)

- PacketConn/UDP 数据报维度(roadmap 已定);
- 自研 Link/上下文对象(选 net.Conn 的对立面);
- 进程外插件契约(gost 双轨制的第二轨,产品化阶段再议);
- 超过 6 个的核心接口(Ingress 不单独设接口,复用 net.Listener + Server 约定)。

### 3.7 留给你决策的三个开放点

1. **Dailer 与 Transport.Dial 是否合一?** 统一后 star 隧道就是一个"dial 到内网"的 transport,概念更少;分开则保留"客户端程序"与"传输"的语义差别(现有配置 dailerList 绑定凭据)。倾向:合一接口、两套实现注册名,配置层仍分表;
2. **Meta 放接口还是随 context?** 倾向接口(显式);若未来 handler 链要中途改写 Target,Meta 字段需可变或提供 WithTarget 派生方法;
3. **pmux 嗅探分发**(nps 调研 §2.1)做成 Transport 装饰器还是独立 Ingress 类型?倾向后者,与 M5 一起落地。
