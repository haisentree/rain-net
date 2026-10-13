# E-track(学习线):用 Go 重实现 EasyTier 的组网内核

- 状态:pending
- 依赖:无硬依赖,与 R 线并行(承载层直接复用 R4–R6/R9 的 transport)
- 关联:`example/EasyTier`(参考克隆,**LGPL-3.0,只读**)、`docs/references.md`、R 线 conformance 文化
- 立项依据:`docs/design.md` §十.6(2026-10-08)

## 背景与定位

rain-net 以学习为导向(roadmap §一),mesh 组网是把加密信道、NAT 穿透、分布式路由串起来的高密度学习块,且 transport 层已有的 KCP/QUIC/mux 经验可整体复用。本 track 用 Go 重实现 EasyTier 的**功能内核**——是"学习重实现",不是"产品复刻":EasyTier 当需求规格书与疑难急救手册,不追它的产品面。

**与主线的关系**:物理隔离。代码不进协议转换网关的装配链;E4 毕业后才考虑以 `Dial(虚拟IP) net.Conn` 用户态接口反哺 Transport 抽象(自建 underlay),且属独立决策。

## 范围边界

**做**:TUN、加密信道、中继、NAT 打洞、路由同步、子网代理(见阶段表)。

**不做**(产品外壳,学习密度低):Web 控制台、中心化管理台、GUI、WireGuard 门户、与官方 config server(22020)的对接。KCP/QUIC 承载不重做——直接复用 `internal/transport/{quic,kcp}`。

## 阶段总览(毕业线 = E4)

| 阶段 | 内容 | 学习目标 | 规模 |
|------|------|----------|------|
| E1 | TUN 设备 + 两节点直连虚拟 IP | Go TUN 操作(wireguard-go/tun)、IP 包读写、接口路由配置 | 小 |
| E2 | 加密信道:密钥交换 + AES-GCM/Noise 封装 | 安全信道构造、重放防护、包格式设计 | 中 |
| E3 | 中继:公网节点转发两个 NAT 后节点 | 中继协议、认证中继、转发模型 | 中 |
| E4 | **NAT 穿透**:STUN 探测 + UDP 打洞,失败回落中继 | STUN、打洞时序、addr 猜测;p2p 成功率可测量 | 大 |
| E5(选修) | 组网协议:peer 发现 + OSPF 式路由同步 + 延迟优先选路 | Gossip、路由收敛、版本号防环 | 大 |
| E6(选修) | 子网代理:TUN 包 ↔ 本地子网的用户态转发 | 用户态 NAT、代理 ARP | 中 |

E1–E4 完成即核心毕业;E5/E6 可随时 drop。**代码位置**(独立 Go module / `internal/mesh/`)E1 开工时定案,回填 design.md §十。

## 各阶段交付与验收

### E1:两节点直连
- 交付:mesh 节点进程,创建 TUN、分配虚拟 IP(如 `10.144.0.x/24`),UDP 明文隧道转发 IP 包;
- 验收:两个 netns/容器内 `ping` 对端虚拟 IP 通;TUN 读写无丢包(小流量)。

### E2:加密信道
- 交付:预置密钥 → 会话密钥交换,数据包 AES-GCM(或 Noise)封装,带反重放计数器;
- 验收:抓包全密文;构造重放包被丢弃;错误密钥节点无法入网。

### E3:中继
- 交付:relay 角色(公网、不开 TUN 也可),两 client 经 relay 认证后互通;
- 验收:netns 模拟 NAT 下两 client 经 relay ping 通;relay 侧抓包不可见明文;relay 不持有网络密钥以外的长期凭据。

### E4:NAT 穿透(毕业线)
- 交付:STUN binding 探测 NAT 类型;UDP 打洞(同时向对方映射地址发包);打洞失败自动回落 E3 中继;
- 验收:netns 构造 FullCone/受限锥/Symmetric 三类 NAT,FullCone/受限锥打洞成功走 p2p,Symmetric 回落 relay;输出 p2p 成功率与切换延迟表。

### E5(选修):路由同步
- 交付:peer 互发现(组播/指定种子)、全量连接状态 gossip、按路径延迟选路;
- 验收:3+ 节点,断开一条边后路由在秒级收敛;延迟注入改变最优路径且流量切换。

### E6(选修):子网代理
- 交付:节点声明可代理 CIDR 并在网内广播,对端把该网段路由到本节点,本节点做用户态转发;
- 验收:对端节点可直接访问代理网段内的 netns"主机"。

## 方法论与参照

- **读的清单(先读后写)**:
  - [wireguard-go](https://github.com/WireGuard/wireguard-go):TUN + Noise 标准实现(E1/E2 主要参照);
  - [tailscale](https://github.com/tailscale/tailscale):打洞/DERP 中继,配套工程文章是一流教材(E3/E4 主要参照);
  - [netbird](https://github.com/netbirdio/netbird):Go 写的开源 mesh VPN,功能清单与 EasyTier 基本重合,最直接对照;
  - `example/EasyTier`:`docs/core-architecture.md`、`docs/data-plane-runtime-plan.md` 当规格书;`docs/tcp-proxy-half-close-validation-2026-09-14.md`、`docs/kcp-control-reliability-*.md` 等验证文档当疑难急救手册。
- **测试策略**:netns 单测(参照上游 netns bench)+ Docker 拓扑 e2e;**互操作试验床**:现有 easytier-web 管理台 + 官方镜像节点的 Docker 环境改造为 e2e 环境,自研节点与真 EasyTier 节点组网对打,互操作即正确性测试。
- **许可证纪律**:EasyTier 为 LGPL-3.0——读协议、看设计、引结论均自由;**不移植代码、不复用其命名与包结构**,本文件及后续 E 阶段文档只记录行为级语义。
- **节奏约定**:每个 E 阶段一个分支(`e1-tun`、`e2-secure-channel`…),试验床验收 + 总结文档(归 `docs/research/mesh-*.md`)后才算 done;与 R 线共用"追加式进度日志"。

## 进度日志

- 2026-10-08:立项。来源:example/EasyTier 功能调研(功能异同分析)+ "以学习为目的 Go 重实现"决策;阶段表、毕业线 E4、参照清单定稿。
