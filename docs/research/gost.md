# gost v3 调研笔记:插件机制与二次开发

> 2026-09-10,基于浅克隆源码(test/example/gost、gost-core、gost-x)+ 官方 go-gost/plugin 仓库。
> 结论先行:**gost 是"编译期进程内组件扩展 + 运行期进程外 gRPC 插件"双轨制**。两条轨道覆盖面完全不同:核心数据面(协议/传输)只能编译期扩展;横切面(认证/限流/观测)有完整的进程外插件契约。

## 1. 进程内扩展:泛型注册表 + init() 自注册(编译期)

### 结构
- `gost-core` 纯接口、零第三方依赖;每类组件各有一个 `Registry[T]` 实例(`core/registry/registry.go`),接口带 `Register/Unregister/IsRegistered/Get/GetAll`;
- `gost-x` 是全部实现(~25 handler、~25 listener、~20 dialer、~15 connector),**没有任何集中注册点**,每个包自己:

```go
func init() {
    registry.HandlerRegistry().Register("http", NewHandler)
}
```

- 空白导入集中在主程序一侧(`gost/cmd/gost/register.go`),要哪些组件由最终二进制决定。

### 组件模板(全项目统一,x/CLAUDE.md 有官方描述)
1. `init()` 自注册;
2. 私有 struct 持有 options + metadata;
3. functional options 构造函数(`NewHandler(opts ...handler.Option)`);
4. `Init(md md.Metadata)` 用 `mdutil.GetBool/GetInt/GetString/GetDuration` 按多个候选 key 提取配置(如 `observePeriod` / `observer.period` / `observer.observePeriod`)。

### 二次开发的实际路径
- **没有任何运行时加载新协议/传输的能力**(无 yaegi、无 hashicorp/go-plugin、无 Go plugin 包);
- 也没有 Caddy xcaddy 那样的"组合构建"工具。二次开发 = 自己写 `main.go` + 自己的 `register.go`(空白导入 x 的部分组件 + 自己的组件),编译自己的二进制;gost 可以整体当库用;
- 运行时可变性由另一条路承担:RESTful 配置 API(`gost-x/api`),改的是配置值,不是能力集合。

## 2. 进程外插件:gRPC/HTTP 服务契约(运行期)

[go-gost/plugin](https://github.com/go-gost/plugin) 是官方插件 SDK,定义的**不是 Go interface,而是 protobuf/gRPC 契约**(同时支持 HTTP/JSON)。gost 作为宿主进程,通过网络调用外部插件服务,配置里用 `plugin:` 块声明(type + 地址)。

覆盖的横切面(均在 `gost-x/<面>/plugin/grpc.go` 有宿主侧实现):

| 服务 | 用途 |
|------|------|
| auth | 客户端认证 |
| admission | 按地址准入控制 |
| bypass | 特定 host 绕过代理链 |
| limiter/traffic | 连接/带宽限速 |
| observer | 连接与流量事件通知 |
| recorder | 流量记录 |
| resolver / hosts | 自定义 DNS 解析 / 域名映射 |
| sd | 服务发现 |
| hop / router | 自定义节点选择/路由 |
| ingress | 入口规则管理 |
| p2p | P2P 隧道策略(NAT 穿透逻辑放在插件进程) |

关键边界:**核心数据面(listener/handler/dialer/connector)没有进程外插件契约**——不可能用外部服务新增一种传输或协议,这是刻意的:数据面每包都过插件会牺牲性能。

## 3. 评价

**优点**
- core/x 分层干净,接口粒度细(Dialer 与 Connector 分离、Multiplexer 是 Transporter 的可选能力、Bind() 统一反向隧道),横切面全部接口化;
- 横切面走进程外插件:插件可用任意语言写、独立部署、崩溃不连累宿主、天然适合多租户产品的认证/计费/限流外置;
- `Registry[T]` + Unregister 为热卸载留了口子;组件模板统一,读一个就会读全部。

**局限 / 风险**
- 二次开发 DX 差:Caddy 有 xcaddy 一条命令组合插件,gost 要手写 main 和 register.go,没有官方脚手架;
- 进程外插件只覆盖横切面,用户无法不编译就扩展数据面;
- `gost-x` 官方自述"无测试,验证靠 build + vet"——接口设计好但实现质量无回归保障,rain-net 已有的 e2e 测试习惯是相对优势,借鉴设计时别把"无测试"也学去。

## 4. 对 rain-net 的启示

1. **双轨制值得照抄**:pluginer(Caddy 模型)管数据面扩展 = 编译期 registry;若将来做对外产品,横切面(认证/限流/观测/计费)按 go-gost/plugin 的方式定义 gRPC 契约外置——正好符合 roadmap 中"代理分组/IP 池/多租户"的产品方向;
2. **把 gost 没做好的补上**:为 rain-net 提供类似 xcaddy 的组合体验其实很轻(我们的注册就是空白导入,写一个 `cmd/rainnet/register.go` 模板 + 文档即可),这是低成本高回报的 DX;
3. **metadata 多候选 key 的容错读取**(`mdutil` 按 key 列表降级)可直接抄进配置解析;
4. 与本项目现状对照:rain-net 的 pluginer 按 ServerType 分组注册 ≈ gost 每类组件一个 Registry,粒度相同;rain-net 缺的是横切面接口化(Auth/Limiter/Observer 目前内嵌在 starserver),M1/M6 时按 gost-core 的横切面包结构补接口。

## 5. 遗留问题(下次深读)

- `gost-x/api` 配置热更新 API 的实现方式(与 rain-net admin API 对照);
- `chain.Transporter` 的 Multiplexer/Bind 具体接线(M1 Transport 接口定稿前必须读);
- `hop` + `selector` + `Marker` 的故障转移实现(对照 connecttable 的 round-robin)。
