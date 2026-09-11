# nps 调研笔记

> 2026-09-10,基于源码 test/example/nps(浅克隆,[ehang-io/nps](https://github.com/ehang-io/nps),已归档)。
> 结论先行:nps 用 **~9.8K 行 Go**(与 rain-net 体量相当)做齐了 Web 管理台 + 多用户 + 多模式隧道 + 端口复用,是"小体量做出完整产品形态"的最佳对照。已归档、无测试、安全设计陈旧——**参考功能清单与个别机制,不参考工程实践**。

## 1. 整体架构

```
npc(客户端)                        nps(服务端,beego v1 框架)
  │  一条桥接端口(tcp 或 kcp)        │
  ├─ WORK_MAIN   信号连接(裸 conn)  → Bridge.Client[id].signal
  ├─ WORK_CHAN   数据隧道(nps-mux)  → Bridge.Client[id].tunnel
  ├─ WORK_FILE   文件隧道(独立 mux) → Bridge.Client[id].file
  ├─ WORK_CONFIG 客户端推送配置      → 建客户端/域名/隧道
  ├─ WORK_REGISTER  IP 注册限时白名单
  ├─ WORK_SECRET    保密隧道密钥匹配
  └─ WORK_P2P       UDP 打洞协调
```

- **一个客户端最多三条物理连接**(signal / tunnel / file),由认证后发送的 flag 字节分发到同一 `clientId`。对比:frp 与 rain-net 的 star 都是单控制通道上多路复用——nps 的三连接模型导致 ping 检活要分别看 signal 和 tunnel,重连一致性差,**反面教材**;
- 认证:客户端版本号必须与服务端**完全相等**才继续(升级强耦合,运维痛苦),然后 32 字节 vkey 查 JSON 文件库得 clientId,握手用 md5。**桥接通道默认无 TLS**(web 口可选 TLS)——rain-net 的 ctrl 通道 TLS 是相对优势;
- 存储:JSON 文件库(`conf/clients.json`、`hosts.json`、`tasks.json`)+ 每条目锁,无数据库、无迁移。产品早期形态可参考"配置即文件",多租户下不可持续;
- 健康检查:**客户端侧探测**本地目标,上报服务端,服务端把不健康 target 从 `TargetArr` 摘除、恢复时加回,实现故障转移。rain-net connecttable 目前只在流下线时切换,基于健康的摘除是低成本增强。

## 2. 值得参考的机制

### 2.1 pmux:单端口多协议分发 ★★★(与传输转换主轴直接相关)
`lib/pmux/pmux.go`,166 行,机制:
1. Accept 后读**前 3 字节**,转数字匹配 HTTP 方法前缀(GET/POST/HEAD/…按字节编码);
2. 是 HTTP → 继续读到 `Host:` 头:host 等于 `web_host` → Web 管理台,否则 → HTTP 代理;
3. 是魔数 `TST` → nps 客户端桥接;
4. 其他(TLS 握手 0x16 开头)→ 默认 HTTPS;
5. 嗅探到的字节重放(`newPortConn(conn, rs, readMore)`)塞进对应虚拟 listener 的 channel,四个服务各自 `GetXxxListener()` 拿到标准 `net.Listener`。

**同一端口同时承载桥接、Web 管理台、HTTP 代理、HTTPS 代理**。这就是"协议嗅探 + 分发 + 字节重放"的最小完整实现,直接可抄进 rain-net 作为一种 ingress listener 类型(对外只开一个端口的部署形态非常实用;gost 也有同类端口复用)。注意它只支持 TCP,且分发发生在无 TLS 的明文上——若前端挂 TLS 需先解 TLS 再嗅探(即 TLS 终止 + SNI/ALPN 分发,可做成增强版)。

### 2.2 客户端推送配置(WORK_CONFIG)★★☆
npc 可携带 `npc.conf` 连上服务端**自动创建自己的客户端、域名、隧道**(`getConfig`),实现"客户端配置文件驱动、服务端零手工配置"的部署体验。与 rain-net 现有方向互补:admin API 是服务端集中管理,dailer 侧再加"推送注册"就是 nps 这种自助接入体验。注意它的权限边界处理粗糙(`ConfigConnAllow` 开关),抄机制时权限模型要重做。

### 2.3 桥接协议与传输解耦 ★☆☆
`bridge_type=tcp|kcp` 一个配置项切换,桥接协议本身只依赖 `net.Conn`(kcp-go 的 Listener 也是 net.Listener)。再次验证 rain-net 的 Transport 抽象方向:上层协议只认 net.Conn,传输随便换。

### 2.4 多租户功能清单(产品面)★★☆
`conf/nps.conf` 即产品功能清单,多租户相关:游客 vkey(`public_vkey`)、用户注册/登录开关、每客户端限速(`allow_rate_limit`)、流量配额(`allow_flow_limit`)、隧道数/连接数上限、多 IP 绑定控制、HTTP 缓存、`http_add_origin_header`。做内网穿透服务产品时的需求清单比设计本身有价值。

### 2.5 P2P 打洞协调(WORK_P2P)★☆☆
服务端通过 signal 通道向两侧下发 UDP 打洞地址与密钥,协调 npc 之间直连(走独立 p2p 端口)。远期加餐参考,机制在 `bridge.go:278` 一处,很浓缩。

## 3. 不抄清单

- beego v1(框架已停止维护)+ 服务端模板渲染 Web 台——rain-net 已选内嵌静态 SPA + REST,方向更对;
- 版本号严格相等校验、md5 握手、明文桥接、默认弱口令(admin/123)——安全面整体陈旧,该项目有历史安全通告,做产品前必须当作反面清单过一遍;
- 三物理连接模型与 `LoadOrStore` 半初始化 struct(`NewClient(nil, nil, c, vs)`)的竞态隐患;
- 全局 JSON 库 + 逐条目锁,写多时争用明显;无任何测试。

## 4. 对 rain-net 的行动项

1. **pmux 式端口复用 listener**:加入 M5(bridger)范围,当作"嗅探分发"型 ingress 的第一个实现(约 200 行量级);
2. **dailer 侧配置推送注册**:在 admin API 之外提供"客户端凭据 + 本地配置 → 自动注册"路径,权限模型参照 §2.2 的教训重新设计;
3. **基于健康检查的故障转移**:connecttable 增加客户端上报健康 → 摘除/恢复 target,与现有 round-robin 兼容;
4. 多租户限速/配额接口化时,拿 §2.4 清单对需求;
5. 桥接协议transport 无关性再次确认 Transport 抽象决策,无需额外动作。

## 5. 遗留问题

- `nps-mux`(外部模块 ehang.io/nps-mux)的窗口/流控实现未读——若 star 补流控时 yamux 对照不够,再看它;
- web/controllers 的会话与权限模型(仅做负面清单用,不细读)。
