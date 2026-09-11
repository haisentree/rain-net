# rain-net 网络模型与项目结构(重构定稿)

> 2026-09-10。本文是重构的目标态设计:**模型定稿 + 命名规范 + 目录结构 + 迁移映射**。
> 论证过程见 `docs/core-abstraction.md`(抽象模型调研)与 `docs/research/*`(逐项目分析);路线图见 `docs/roadmap.md`。两文冲突时以本文为准。

## 一、深度分析:各项目给我们的结构性结论

| 项目 | 值得继承的一条 | 值得规避的一条 |
|------|--------------|--------------|
| gost v3 | 角色最小接口 + 泛型注册表 = 配置驱动产品与插件系统天然合一 | 接口过度细分(Dialer/Connector/Handshaker/Multiplexer/Binder 五合一 Transporter),概念成本高 |
| sing-box/v2ray | transport 与协议正交,任意组合 | 自研 Link 内部链路,第三方生态资产(http.Server 等)无法直接挂载 |
| frp | 隧道消息协议与重连治理成熟 | 隧道模型与网关模型割裂:frp 做不了协议转换,两套世界观 |
| nps | pmux 单端口协议嗅探分发;小体量做齐产品面 | 三条物理连接的客户端模型,检活/重连一致性差 |
| Caddy | 插件生命周期钩子 + Context 共享状态 + 热更新 | —— |
| Envoy | transport_socket 证明"传输可替换"值得一个独立扩展点;listener filter 与 network filter 分层(嗅探属前者) | —— |
| yamux/quic-go | 多路复用做成 `Accept/Open → net.Conn` 形状,上层无感知 | —— |
| CoreDNS | ServeDNS 链式约定极简稳定 | —— |

三条最重要的综合推论:

1. **隧道不应该是独立的世界观**(frp 教训):star 隧道拆成"入站的 star 协议 handler + 出站的 star dialer + 共享的流注册表",frp 式功能就只是 routes 里的一条规则——隧道与网关统一在同一个模型里;
2. **通用语言必须是扩展 net.Conn**(v2ray 反例):生态兼容是"任意 × 任意"免费组合的来源;
3. **接口数量设上限**:gost 的教训。核心接口 ≤ 6,宁可角色内聚,不做细粒度分解。

## 二、模型定稿

```
                    ┌─────────────────────────────────────────┐
                    │              Transport 层                │
                    │   tcp │ tls │ ws │ h2 │ quic │ ...      │
                    └───────┬─────────────────────┬───────────┘
                            ▼                     ▼
   外部用户 ──► Ingress ──► Handler 链 ──► Router ──► Dialer ──► 目标
            (listener)   (pluginer,   (routes    (direct/
                          解析协议,    规则匹配,   star 隧道/
                          填充 Meta)   选出站)    转发链…)
                            ▲                     │
                            └────── Bridge ◄──────┘
                          (双向 io.Copy + 半关闭传播)
```

一句话:**传输正交、协议走链、路由选向、桥接转换;隧道只是又一种出站。**

六个核心角色(接口总数上限):

| 角色 | 接口 | 职责 | 对应旧概念 |
|------|------|------|-----------|
| Transport | `Listen/Dial → net.Listener/net.Conn` | 字节搬运,两端可插拔 | transport 字段(硬编码 switch) |
| Ingress | `net.Listener` + 绑定 handler | 接入,不做协议 | listenerList.type 的 proxy/tcp/udp |
| Handler | pluginer 插件链(不变) | 协议语义:认证、解析目标、填充 Meta | handlerList;**star 服务端协议和 admin 也归此层** |
| Router | 规则匹配 `(ingress, identity, target) → dialer` | 转发决策 + round-robin/故障转移 | ctrlproxy settings.connect / connectTable |
| Dialer | `Dial(ctx, target) (Conn, error)` | 出站:直连 / star 隧道流 / 转发链 | dailerList(修正拼写) |
| Bridge | 两条 Conn 互拷 + 半关闭传播 | 协议转换的执行者 | bridgerList(占位) |

三个开放点的定稿决策(替代 core-abstraction.md §3.7):

1. **Dailer 与 Transport 不合一**。Transport.Dial 是裸管道(无路由语义),Dialer 是带凭据与身份的路由出站,是产品概念(客户端凭据表挂在它身上);配置仍分两张表。
2. **Meta 走接口,不用 context**。字段定名:`Listener / Remote / Identity / Target`;Meta 只读,协议 handler 通过 `Conn.SetTarget()` 派生(见下 Conn 定义)。
3. **pmux 嗅探是独立的 ingress 装饰器**,归 `internal/ingress/sniff`,M5 与 Bridge 同批落地,不做成 transport。

### 核心接口(定稿签名)

```go
// pkg/rainnet —— 组件之间的通用语言(可被第三方插件导入)
type Conn interface {
    net.Conn
    CloseWrite() error        // 半关闭;不支持返回 ErrNotSupported
    Meta() Meta               // 只读元数据
    SetTarget(target string)  // 协议 handler 解析出目标后写入;仅允许调用一次
}

type Meta struct {
    Listener  string // 进入的 ingress 名
    Remote    string // 真实源地址
    Identity  string // 协议层认证出的身份(star: clientProxyName)
    Target    string // 协议层解析出的目标
}

// internal/transport
type Transport interface {
    Listen(ctx context.Context, addr string) (net.Listener, error)
    Dial(ctx context.Context, addr string) (net.Conn, error)
}
// 多路复用实现(h2/quic/star/ws)内部私有连接池,Dial 返回逻辑流,接口不变。

// internal/dialer
type Dialer interface {
    Dial(ctx context.Context, conn rainnet.Conn, target string) (rainnet.Conn, error)
}
// 入参带上入站 Conn:star dialer 需要读取 Identity 选择流;direct dialer 忽略。

// internal/bridge
type Bridger interface {
    Bridge(src, dst rainnet.Conn) error // 内部双向 CopyBuffer + 半关闭传播
}
```

### 半关闭传播契约(Bridge 实现,统一收编 star 已有的排空逻辑)

| 源侧事件 | 目的侧支持 CloseWrite | 目的侧不支持(ws) |
|---------|----------------------|------------------|
| `CloseWrite()` | 转发 `CloseWrite()`,继续读 | 记"已关写"标记,继续读 |
| 读到 EOF | `CloseWrite()` 后排空对端在途数据再关 | 排空后整条关闭(降级) |
| 读错误/取消 | 立即双关 | 立即双关 |

> Bridge 概念专题调研、业界对照(gost Transport / frp Join / nps CopyWaitGroup)、取其精华去其糟粕与阅读指引见 `docs/research/bridge.md`。

## 三、命名规范(字段命名根据模型来)

**原则:配置字段、代码标识符、文档三者同词;采用 Go 生态标准英文,修正既有拼写错误。**

| 旧(现状) | 新(定稿) | 说明 |
|-----------|-----------|------|
| `dailerList` / `dailer-*` | `dialers` / Dialer | **修正拼写** dailer→dialer(net.Dialer 标准词) |
| `bridgerList` / `bridger` | `bridges` / Bridge / Bridger | bridger 非标准英语词 |
| `listenerList` | `ingress` | 与模型角色同名;进程内代码 Ingress |
| `handlerList` / `plugins` | `handlers` / `plugins` | 保持 |
| `settings.connect` | 顶层 `routes` | 路由从单个 listener 的设置提升为服务级一等概念 |
| `clientProxy` / `clientProxyName` | `credentials` / `identity` | 凭据即身份;不再叫"客户端代理" |
| `streamId` | `stream` | 去掉无信息量的 Id 后缀 |
| `keyPassword` | `keyPassword`(保留) | 隧道凭据语义清晰 |
| `ctrlproxy`(listener type) | handler `star` | 隧道服务端是协议,不是监听器类型 |
| `proxy`(listener type) | 无(纯 ingress + routes 指向 star dialer) | 反代入口不再需要特殊类型 |
| `admin`(listener type) | handler `admin` | 管理面统一为 handler |
| cmd `star` / `starclient` | `raind`(服务端)/ `rainc`(客户端) | 单一产品命名,参照 sshd/ssh 缩写习惯 |

### 关键命名决策:入口叫 Ingress,不叫 Listener(已定稿)

入口侧存在两个概念,必须用两个词分开:

- **形状**:`net.Listener`——Transport.Listen 返回的接口,**承诺永不偏离**(生态兼容红线的来源);
- **角色(接入点)**:Ingress = 一个 listener + 绑定的 handler 链 + settings,是配置与装配层面的概念,**不设接口**。

选 Ingress 的两条理由:

1. **消歧义**:避免 包名 `listener` / 类型 `Listener` / 标准库 `net.Listener` 三层同词嵌套(gost 的 `listener.NewListener` 返回 `listener.Listener` 即此弊)。Ingress(角色)持有 Listener(形状),词各归其位;
2. **命名反向塑造接口演化**:叫 Listener 的角色会自然长出自定义接口——gost 的 Listener 正因此偏离 net.Listener 形状,无法直接喂给 `http.Server.Serve(l)`。叫 Ingress 则从名字上钉死"下面压着的永远是 net.Listener",给"形状不偏移"多一道命名层面的保险。

代价(接受):与 gost/Caddy/Envoy 词汇不同,读它们的代码或搬运实现时需心里做一次 Ingress ↔ Listener 映射。

### 目标配置形态(target schema)

```yaml
service:
  - name: gateway
    ingress:
      - name: socks-in
        transport: tcp            # tcp|tls|ws|h2|quic
        addr: 0.0.0.0:8085
        handler: socks-chain

      - name: star-in             # 原 ctrlproxy
        transport: tls
        addr: 0.0.0.0:5172
        handler: star
        settings:
          credentials:            # 原 clientProxy 清单
            - name: client-0
              keyPassword: pwd-0
              streams: [svc-a, svc-b]

      - name: admin-in            # 原 admin
        transport: tcp
        addr: 127.0.0.1:5173
        handler: admin
        settings: { adminPassword: admin-secret }

handlers:
  - name: socks-chain
    plugins: [socks5]

dialers:                          # 原 dailerList
  - name: office                  # star 隧道出站
    type: star
    transport: tls
    addr: 203.0.113.10:5172
    identity: client-0
    keyPassword: pwd-0
  - name: lan                     # 直连出站
    type: direct

routes:                           # 原 settings.connect,提升为服务级
  - ingress: socks-in             # 匹配维度:ingress / identity / target(通配省略)
    to: office
    streams: [svc-a]              # star 目标流,多条 round-robin + 故障转移
```

## 四、项目目录结构

```
rain-net/
├── cmd/
│   ├── raind/                    # 服务端入口:main + register.go(空白导入装配)
│   └── rainc/                    # 客户端(隧道 dialer)入口
├── pkg/                          # 允许第三方插件导入的稳定库
│   ├── rainnet/                  # Conn 接口、Meta、适配器(netConnAdapter / starStreamAdapter)
│   └── pluginer/                 # 插件框架(现 pluginer/ 迁入)
├── internal/
│   ├── transport/                # Transport 接口 + Registry
│   │   ├── tcp/
│   │   ├── tls/
│   │   ├── ws/                   # M2
│   │   ├── h2/                   # M3
│   │   └── quic/                 # M4
│   ├── handler/                  # 协议 handler 插件(数据面,服务端自带)
│   │   ├── socks5/
│   │   ├── httpproxy/
│   │   ├── star/                 # 隧道服务端协议:握手/注册/流注册表(现 hub+streamtable+ctrlproxy)
│   │   └── admin/                # 管理 API + 内嵌 Web(现 admin_server/admin_web/web/)
│   ├── dialer/                   # 出站
│   │   ├── direct/
│   │   └── star/                 # 隧道出站:connOpen/流适配(现 dailer.go 出站侧)
│   ├── router/                   # 路由表(现 connecttable 升级:匹配/round-robin/故障转移)
│   ├── bridge/                   # Bridger + 半关闭传播(M5)
│   ├── ingress/                  # listener 装配、服务生命周期(现 register.go/server.go)
│   │   └── sniff/                # pmux 式嗅探分发装饰器(M5)
│   └── client/                   # rainc 客户端域:本地服务拨号、凭据、重连(现 cmd/starclient+starserver/dailer.go)
├── protocol/                     # 线上协议纯库(无装配,保持现状)
│   ├── star/                     # 帧/会话/握手
│   └── ws/
├── config/                       # 配置模型定义 + 加载/校验(含新旧 schema 兼容层)
├── etc/                          # 示例配置(按新 schema 重写 star.example.yaml)
├── docs/                         # roadmap / references / research / design(本文)
└── test/
    ├── e2e/                      # 端到端测试(现 proxy_e2e_test 等迁入)
    └── example/                  # 参考项目克隆(已 gitignore)
```

结构决策说明:

- **pkg/ 只放两样**:rainnet(第三方 handler 插件必须导入)与 pluginer(写插件必用)。其余全 internal,保持公共面最小;
- **protocol/ 保持纯库**:不含任何装配逻辑,go.mod 不引用内部包——协议可独立测试、未来可单独发版;
- **handler/dialer/transport 各自子目录一个实现**(gost x 的按角色分目录模式):每个传输/协议都是"目录即插件",加新协议 = 加目录 + init 注册,不动骨架;
- **client 独立成域**:客户端(内网侧)与服务端共享 protocol/pkg,但装配完全分开,避免现在 starserver 里 dailer.go 混居的状况;
- **cmd/ 清理**:mp、net-test、net2、etcd、problem、test、trans 等实验代码移出主仓库或删除(重构时一并处理);
- **test/e2e**:现 proxy_e2e_test.go 是重构最重要的安全网,迁移后按新 schema 重写用例配置。

## 五、迁移映射(旧 → 新)

| 现有物 | 去向 |
|--------|------|
| `internal/star/starserver/server_ctrl_proxy.go` | `internal/handler/star/`(服务端协议) |
| `ctrl_hub.go` + `streamtable.go` | `internal/handler/star/`(流注册表,对上叫 TunnelHub → 定名 `StreamRegistry`) |
| `connecttable.go` | `internal/router/` |
| `proxy_server.go` | 拆:监听部分 → `internal/ingress`;转发部分 → `internal/bridge` |
| `dailer.go`(服务端侧)/`dailer_test.go` | 客户端域 → `internal/client/` |
| `protocol/star/dailer.go`、`ctrlclient.go` | `internal/dialer/star/` 出站适配 + `internal/client/` |
| `admin_server.go`、`admin_web.go`、`web/` | `internal/handler/admin/` |
| `register.go` 的 type/transport switch | `internal/ingress/` 装配 + Registry 查表 |
| `pluginer/` | `pkg/pluginer/` |
| `internal/star/plugin/{socks5,httpProxy,...}` | `internal/handler/{socks5,httpproxy,...}` |
| `internal/custom`、`internal/dns` | 暂留原地,DNS 按 transport+handler 模型迁移(独立里程碑) |

## 六、数据面全景与多链 Bridge 演示

```
┌─────────────────────── 控制面 ──────────────────────────────────┐
│   外部管理端/CLI ──► admin handler(ingress+handler)             │
│         也支持 ──► 配置中心 configStore(file/etcd,M6)           │
│         职责:routes 增删 / 凭据发放吊销 / DNS zones / kick       │
└──────────────────────────┬─────────────────────────────────────┘
                           ▼ 生效(内存立即 / watch 分发)
┌────────────────────────── 数据面 ───────────────────────────────┐
│                    Transport 层(可插拔)                         │
│         tcp │ tls │ ws │ h2 │ quic │ ...                       │
│           │ Listen                    ▲ Dial                   │
│           ▼                           │                        │
│  Ingress ─► Handler 链 ─► Router ─► Dialer ─► 目标/下一跳        │
│       ▲                                          │             │
│       └──────────── Bridge.Bridge(src, dst) ◄────┘             │
│              (半关闭传播 / 记账 / 限速装饰器)                     │
└─────────────────────────────────────────────────────────────────┘
```

四个场景覆盖产品全部形态,核心演示点是 **Bridge 如何把多条链关联起来**:

**场景 A:基础转发**——一条链,Bridge 接两端:

```
用户 ─tcp─► [socks-in] ─► socks5 ─► router ─► [dialer lan] ─► 内网目标
                                    └── bridge(src, dst) ──┘
```

**场景 B:传输转换**(产品主轴)——Bridge 两端换 transport,协议不动:

```
用户 ──ws──► [ws ingress] ─► socks5 ─► router ─► [direct + quic transport] ─► 目标
                │                                    │
          ws 帧还原成流                        quic stream 适配成流
                └──────── bridge(两条"流",传输已无关)────────┘
```

Bridge 两端是 rainnet.Conn,ws 帧和 quic stream 都已还原成字节流——"传输转换"只是两端各换了 transport 实现,Bridge 与协议层对此零感知。

**场景 C:多节点串联**(server ↔ server)——模型的自相似复制:

```
用户 ─► 节点A [ingress→router→dialer star] ══隧道══► 节点B [star handler 收流] ─► router ─► direct ─► 目标
```

节点 B 眼里,隧道进来的流量就是普通 ingress Conn——**每个节点都是同一个模型的完整复制,节点间隧道对 Bridge 只是又一条 Conn**。多跳中转 = Router 的 dialer 指向下一跳节点。

**场景 D:隧道星型**(star 服务端 + 多 dailer,现有功能归位):

```
dailer1(内网1)──┐
dailer2(内网2)──┼──► [star-in + star handler] ──► StreamRegistry(流注册表)
proxy-in ──► router(round-robin: streams[svc-a]) ──► [dialer star] ──┘
```

## 七、控制面设计:复杂组件的外置 API 配置

DNS 这类组件(线路匹配、缓存同步、上游选择)静态配置装不下,需运行时经 API 配置。四条原则:

1. **管理 API 不另起端口,组件自注册子路由**。admin handler 是唯一控制面入口,组件实现可选接口挂载自己的 API:

   ```go
   type APIProvider interface {
       RegisterAPI(mux *http.ServeMux) // DNS 挂 /api/dns/{name}/zones;star 挂 /api/tunnel/...
   }
   ```

   鉴权统一走 admin 的 Bearer token(deny-by-default 已实现),组件不做自己的鉴权;现有 `/api/clients`、`/api/kick` 等平移为 `/api/tunnel/*` 命名空间。插件拔掉,它的 API 路由随组件一起消失,零残留。

2. **组件自治,框架只管分发**。动态配置格式由各 handler 自定义(DNS 的 zone/records、Router 的 rules 互不知晓),组件实现 `Apply(patch []byte) error` 接收自己的变更——"协议 handler 插件自治"在控制面的对偶。

3. **生效与持久化分两级**。运行时状态改内存立即生效(默认语义:重启以文件为准);可选 `ConfigStore` 后端持久化:

   ```go
   type ConfigStore interface {
       Get(ctx, key) ([]byte, error)
       Put(ctx, key, val []byte) error
       Watch(ctx, key) <-chan Event // 多节点同步的关键
   }
   // 实现:fileStore(默认)/ etcdStore(多节点,client v3 已在 go.mod)
   ```

4. **多节点同步靠 watch**。节点 A 发的 DNS 配置经 configStore watch 广播到节点 B 的同名 handler Apply;缓存失效用版本号/revision。对应 README 规划的"缓存监听与同步"。

DNS 示例走查:`ingress(udp/tcp transport) + dns handler`;查询在 handler 内闭环(缓存命中直接答,不进 Bridge);未命中按线路选上游——上游是 dialer,**DoT/DoH = 同一个 dns handler 配 tls/http transport 出口**,再次吃到传输正交红利;records 经 `/api/dns/*/zones` 外置管理;"限速限评"不进 DNS 组件,是 Conn 装饰器(QPS limit)。与 gost 外置 gRPC 插件(横切面外置到独立进程)不冲突:那是产品多租户阶段的第二轨(roadmap 已定),本节是"管理面外置给外部系统"。

## 八、重构执行顺序(与 roadmap 合并)

1. **R0 安全网先行**:e2e 测试迁到 test/e2e 并保证全绿(重构期间唯一验收标准);
2. **R1 = M1**:pkg/rainnet + internal/transport(tcp/tls)+ register 查表化;修 plugin/forward、zplugin 编译错误;交付 test/conformance 一致性套件(细则见 §九.1);
3. **R2**:router(现 connecttable 迁移)+ bridge 占位 + 半关闭传播契约实现;
4. **R3**:star 拆解((handler/star + dialer/star + internal/client)+ 配置新 schema + 兼容层;admin API 改造为聚合点(引入 APIProvider,现有路由平移到 /api/tunnel/*);
5. **R4+**:按 roadmap M2(ws)→ M3(h2)→ M4(quic)→ M5(bridge 全量 + sniff)推进;M6 落地 ConfigStore(file/etcd)+ DNS 按 transport+handler 模型迁移并接入外置配置 API。

> 规则:每个 R 步骤一个分支、e2e 全绿才合入;schema 变更必须同时更新 `etc/` 示例与本文档。

## 九、定稿补充(2026-09-11,随 R 步骤落地的六项细则)

模型定稿时实现者视角复查出的细则,均已分配到对应 R 步骤:

1. **传输一致性测试套件(conformance suite)**——`test/conformance/`:同一组场景(echo、大流量、半关闭排空、多流并发、deadline、ctx 取消)参数化跑在每一个 Transport 实现上,作为半关闭契约的机器保障。归 **R1** 交付,此后 M2–M4 每个新传输以通过同一套件为验收标准;
2. **star 帧半关/全关区分**:现有 `streamClose` 一帧两义,starStreamAdapter 需要区分 `CloseWrite()`(仅停上行)与 `Close()`(整条终止)——R3 拆解时在 reserved 位加标志或新增消息类型,并同步 `docs/star-protocol.md`;
3. **取消传播契约**:每条连接持有从 ingress 派生的 ctx,Bridge 双向拷贝监听取消;组件契约:"收到的 ctx 被取消时,必须在有限时间内释放连接"。M6 热重载的旧实例排空依赖此链;
4. **可观测性最小集**:Meta 增加 `ID` 字段(进程内原子计数),日志贯穿 ingress→handler→router→dialer→bridge 同一连接 ID;错误分三类(网络错/协议错/策略拒绝)为 metrics 留维度;
5. **Router 匹配优先级**:按配置声明顺序匹配、首中即停,不做最长匹配——可预测且与 admin API 动态增删的顺序语义一致。归 R2 定案;
6. **安全与资源默认值**:示例配置 ingress 显式声明绑定地址(不用 0.0.0.0 兜底);基础 Conn 装饰器清单定为:限速、记账、admission(单 ingress 连接数上限、单 identity 速率上限)。
