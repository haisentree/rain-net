# 开源参考项目调研笔记

> 2026-09-10 建立。配合 `docs/roadmap.md`(定位与路线图)使用。
> 用途:为"多协议服务 + 传输协议转换 + 插件机制"各模块寻找实现参考。
> 约定:每个项目深读后,在 `docs/research/<项目名>.md` 单独写对比笔记(接口设计、与我们方案的差异、可借鉴点、可 criticisms),本文档只做索引与导读。

## 优先级总览(与里程碑映射)

| 优先级 | 项目 | 对应里程碑 | 参考价值 |
|--------|------|-----------|---------|
| ★★★ | gost v3 | M1(Transport 接口)、全局 | 架构同构度最高,接口拆分范本 |
| ★★★ | wstunnel | M2(ws)、M5(bridger) | 与"传输转换"主轴几乎同构的独立工具 |
| ★★★ | yamux | M1/M6(star 流控) | 流多路复用 + 窗口流控的标准范本 |
| ★★☆ | sing-box | M1、全局 | transport 与协议正交组合的工程实现 |
| ★★☆ | frp | star 隧道封盘前 | 消息协议、重连、负载均衡的同类对照 |
| ★★☆ | nps | M5(bridger)、产品化 | pmux 端口复用、客户端推送配置、多租户清单(已深读 → docs/research/nps.md) |
| ★★☆ | chisel | M2(ws) | ws 承载任意 TCP 流的完整实现 |
| ★★☆ | quic-go | M4(QUIC) | 直接依赖库,读法有讲究 |
| ★★☆ | Reverst | M4(QUIC) | HTTP/3 反向隧道,Go + quic-go |
| ★☆☆ | Xray/v2ray | M3(h2)、M7(kcp) | 传输抽象演进史、xhttp 变体 |
| ★☆☆ | Caddy | M6(产品化) | 热更新、TLS 自动化 |
| ★☆☆ | hysteria | M4(QUIC) | QUIC 代理协议 + 自定义拥塞控制 |
| ★☆☆ | cloudflared | M4/M5 | 生产级 QUIC 隧道实践 |

---

## A. 整体架构:协议 × 传输解耦

### gost v3 —— 同构度最高,第一优先 ★★★
- 仓库:[go-gost/core](https://github.com/go-gost/core)(纯接口)+ [go-gost/x](https://github.com/go-gost/x)(实现),文档 [gost.run](https://gost.run/en/)
- Go。定位与本项目几乎一致:多协议隧道/代理,支持 tcp/tls/ws/wss/h2/h3/quic/kcp/grpc/ssh/relay/masque,协议可自由组合成转发链。
- **接口拆分比我们的 `Transport` 草案更细,重点研读**:
  - `Service` = `Listener` + `Handler`;`Listener` 只管 accept 入站 `net.Conn`,`Handler` 负责认证/路由/转发;
  - 出站侧把 `Dialer`(拨到下一跳)与 `Connector`(在通道上连到最终目标)分离,`chain.Transporter` 打包 Dialer+Connector+Handshaker+**Multiplexer**+Binder——多路复用被建模为 Transporter 的一个可选能力,值得抄;
  - 反向隧道 = `Router.Bind()` 返回远端 `net.Listener`(把"反向"也统一进同一套接口);
  - 泛型 `Registry[T]` 注册表;`Marker`(节点失败计数)配合 `Selector` 做故障转移;
  - 横切面全部接口化:Auth/Auther、Admission、Bypass、conn/rate/traffic 三种 Limiter、Stats、Observer、Recorder。
- core 零依赖、实现全在 x 模块的分层,直接回答了"插件框架与实现如何解耦"。

### sing-box ★★☆
- 仓库:[SagerNet/sing-box](https://github.com/SagerNet/sing-box),文档:[V2Ray Transport 配置](https://sing-box.sagernet.org/configuration/shared/v2ray-transport/)
- Go。inbound / outbound / router 三层;任意代理协议 × 任意传输(tcp/ws/grpc/h2/quic/httpupgrade)正交组合——正是本项目"协议层 × 传输层"决策的成熟样板。
- 参考:transport 接口的 `DialContext/Listener` 形状、mux 多路复用协议选择(smux/yamux/h2mux)、router 规则引擎、内置 DNS 模块。

### Xray / v2ray ★☆☆
- 仓库:[XTLS/Xray-core](https://github.com/XTLS/Xray-core)、[v2fly/v2ray-core](https://github.com/v2fly/v2ray-core)
- Go。transport 抽象演进史(从 mkcp/ws/h2 到 xhttp):看它每个传输如何实现统一的 `internet.Transport` 接口,以及流控/伪装层叠设计。读设计文章比读代码效率更高。

### MOSN ★☆☆(已不活跃,读文档为主)
- 仓库:[alibaba/mosn](https://github.com/alibaba/mosn)
- Go。多协议网络代理框架,xprotocol 协议扩展框架(把"新增一种协议"做成纯插件)、**进程平滑热升级**(graceful upgrade)是招牌,M6 可回来看。

### Envoy ★☆☆(概念参考,不读代码)
- 仓库:[envoyproxy/envoy](https://github.com/envoyproxy/envoy)
- C++。`transport_socket` 抽象(可替换的传输层)是"传输可插拔"的鼻祖;listener filter 与 network filter 分层。读架构文档/blog 即可。

## B. 传输转换专题(与产品主轴同构)

### wstunnel ★★★
- 仓库:[erebe/wstunnel](https://github.com/erebe/wstunnel)
- Rust。专门做"任意 TCP/UDP/socks5 流量 over WebSocket / HTTP2 / HTTP3(WebTransport)"——和 bridger 要干的事高度重合,是最小的同类产品对照。
- 参考:ws 之上自定义帧协议(如何在无半关闭语义的 ws 上表达流控制)、UDP over ws/h3 的封装、path 路由、重连策略。

### Reverst ★★☆
- 仓库:[flipt-io/reverst](https://github.com/flipt-io/reverst),[设计博客](https://blog.flipt.io/so-we-built-reverst)
- Go + quic-go。负载均衡反向隧道 over HTTP/3;体量小,适合整库通读。另有 [gardener/quic-reverse-http-tunnel](https://github.com/gardener/quic-reverse-http-tunnel) 同类可对照。

### chisel ★★☆
- 仓库:[jpillora/chisel](https://github.com/jpillora/chisel)
- Go。HTTP+WebSocket 隧道,反向端口转发,自带 mux。M2 做 ws transport 时对照:帧映射、认证(user/pass + host fingerprint)、多路复用怎么叠在 ws 上。

### socat ★☆☆(概念参考)
- 主页:[dest-unreach.org/socat](https://www.dest-unreach.org/socat/)
- C。"多地址类型间 relay"的开山鼻祖,地址类型抽象(TCP/UDP/EXEC/OPENSSL…)是协议转换的最早形态。知道它、引用它即可。

## C. 隧道 / 反向代理(star 同类,封盘前对照)

### frp ★★☆
- 仓库:[fatedier/frp](https://github.com/fatedier/frp)
- star 的直接同类。参考:消息注册与分发(`pkg/msg`)、smux 叠加 tcp、客户端重连退避、负载均衡与健康检查、server plugin(webhook)、visitor 模式。star 补流控时直接对照。

### cloudflared ★☆☆
- 仓库:[cloudflare/cloudflared](https://github.com/cloudflare/cloudflared)
- Go。生产级 QUIC 隧道:多条 edge 连接管理、QUIC 优先 + http2 降级的 fallback 策略、重试退避。M4/M5 的运维面参考。

### nps ★★☆(已深读,笔记:[docs/research/nps.md](research/nps.md))
- 仓库:[ehang-io/nps](https://github.com/ehang-io/nps)(已归档,源码在 test/example/nps)
- ~9.8K 行 Go 做齐 Web 管理台 + 多用户 + 多模式隧道 + 端口复用。参考:pmux 单端口协议嗅探分发(★,抄进 M5)、客户端推送配置注册、客户端侧健康检查故障转移、多租户功能清单。反面清单:明文桥接、版本严格相等校验、无测试、beego v1。

### rathole / bore / realm ★☆☆
- [rapiz1/rathole](https://github.com/rapiz1/rathole)(Rust,内建 noise 加密,零拷贝性能手段)、[ekzhang/bore](https://github.com/ekzhang/bore)(几百行,control/data 通道分离的极简范本)、[zhboner/realm](https://github.com/zhboner/realm)(极简 relay,bridger 最小实现参考)。

## D. 流多路复用与传输实现(star 流控 + transport 素材)

### yamux ★★★
- 仓库:[hashicorp/yamux](https://github.com/hashicorp/yamux)
- star 补按流流控的直接范本:per-stream 收发窗口、背压、keepalive、**半关闭(CloseWrite)**——最后一点与我们的半关闭约定直接相关。且 `Session.Open/Accept` 返回 `net.Conn`,是"多路复用型 Transport"的接口模板。

### smux ★☆☆
- 仓库:[xtaci/smux](https://github.com/xtaci/smux)
- 与 yamux 对比读:帧合并写、可更新窗口等性能取舍;kcptun/shadowsocks 生态在用。

### quic-go ★★☆
- 仓库:[quic-go/quic-go](https://github.com/quic-go/quic-go)
- M4 的直接依赖。读法:`quic.Connection` 的 OpenStream/AcceptStream、`quic.Stream` 的 `CancelRead`(读侧半关闭)、连接级 vs 流级流控、0-RTT。不重读内部实现,先读 API 语义。

### kcp-go / kcptun ★☆☆(M7 加餐)
- 仓库:[xtaci/kcp-go](https://github.com/xtaci/kcp-go)、[xtaci/kcptun](https://github.com/xtaci/kcptun)
- ARQ + FEC + 拥塞控制,"smux over kcp" 的分层组合方式。

## E. QUIC 代理协议设计

- [apernet/hysteria](https://github.com/apernet/hysteria) ★☆☆:QUIC 代理,Brutal 自定义拥塞控制(如何在 quic-go 上挂自定义 CC)、http3 masquerade。
- [go-telegram/tuic](https://github.com/EAimTY/tuic)、[juicity](https://github.com/juicity/juicity):QUIC 上原生多路复用代理协议的两种帧设计,简单对照。

## F. 插件机制(README 中 pluginer 的延伸)

- [caddyserver/caddy](https://github.com/caddyserver/caddy) ★☆☆:pluginer 的出处(README 已记录);M6 重点看 admin API 热加载(`/load`)与 TLS 自动化(内部 CA、按需签发)。
- [traefik/yaegi](https://github.com/traefik/yaegi) ★☆☆:Go 解释器,插件不编译即可分发——若未来想"用户写插件不发版",先看它。
- [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) ★☆☆:子进程 + gRPC 插件隔离模型,需要进程隔离时的备选。
- [coredns/coredns](https://github.com/coredns/coredns):DNS 插件链(`plugin.Handler.ServeDNS` + `NextOrFailure`),internal/dns 已按此模仿,补测试时对照。

## G. 协议设计补充

- [shadowsocks/shadowsocks-rust](https://github.com/shadowsocks/shadowsocks-rust):AEAD 流协议、TCP stream / UDP associate 双形状;若未来 star 帧层做内置加密,先读它。
- [txthinking/brook](https://github.com/txthinking/brook):单二进制多协议的组织方式。

---

## 调研方法约定

1. clone 后先只读接口定义(gost: `core/` 各包;gost transport: `chain.Transporter`;sing-box: `transport/`),再挑一条最短路径跟一次请求生命周期;
2. 每个项目产出 `docs/research/<项目名>.md`:接口签名摘录 → 请求/连接生命周期图 → 与 rain-net 现方案的差异表 → 决定抄什么/不抄什么及理由;
3. 与 `docs/roadmap.md` 里程碑绑定:做 M2 之前先交 wstunnel + chisel 笔记,做 M4 之前先交 quic-go + Reverst 笔记,避免边做边翻源码。
