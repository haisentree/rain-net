# R8:产品化收尾(= roadmap M6)

- 状态:in-progress(流控 ✅ + ConfigStore ✅ + 热更新 ✅ + DNS 垂直切片 ✅ 已合入;剩可观测性/分发收尾/EtcdStore/DNS 补全,2026-10-09 核对)
- 依赖:R3、R7
- 关联:`docs/design.md` §七(控制面)、§九.3(取消链);`docs/research/nps.md` §2.4(功能清单);roadmap M6

## 目标

从"能用的重构成果"到"成熟产品形态":流控、热更新、配置持久化与多节点同步、DNS 迁移、可观测性、分发。

## 交付物(可拆分独立推进)

1. **star 按流流控/背压** ✅(2026-09-11):Window 原语 + MsgWindowUpdate 双向流控;客户端每连接写泵消除跨流头阻塞;e2e TestFlowControlNoHOL(慢流占满窗口、快流不受影响);proxy 下行排空竞态修复(静默窗口)。**star 协议自此封盘**(协议文档帧表待同步 0x22);
2. **配置热更新** ✅(2026-09-12,R8-2b):实例替换式重载(Caddy 路线)——`pluginer.Instance.Stop()`(关监听/包连接 + OnShutdown 回调 + wg 等待)+ `starserver.Reload` 先 `config.Load` 校验(坏配置保旧实例原样保留)再停旧起新;SIGHUP 触发,SIGINT/SIGTERM 优雅退出;e2e TestReload 全过;
3. **ConfigStore** ✅(2026-09-11,部分):internal/configstore 接口 + FileStore(原子写/轮询 watch/键逃逸防护)+ 测试;EtcdStore 待配置中心落地(接口已预留);
4. **DNS 迁移**:**垂直切片 ✅**(2026-09-12,R8-3)——`internal/handler/dns`:zone 表精确匹配 A 记录应答 + 未命中上游轮询转发 + TCP-DNS 帧循环;APIProvider 挂 `/api/dns/{name}/zones`(即时生效 + 可选 FileStore 持久化,design §七 旗舰示例)。**剩余**:线路匹配(地理/运营商)、上游 dialer 化 → DoT/DoH(tls/http transport 出口)、UDP 入站(受阻于 transport 层 v1 不建模 PacketConn,roadmap 列为远期)、handler 测试补齐(切片目前无独立测试);
5. **可观测性**:metrics(错误三分类、每 transport 连接数/流量)、日志贯穿 ConnID、admin 大盘补充;
6. **分发**:**部分 ✅**(R8-1)——Makefile `dist` 交叉编译矩阵已落(linux/amd64+arm64、darwin/amd64+arm64、windows/amd64,raind+rainc 双二进制,CGO_ENABLED=0 + trimpath);**剩余**:最小 systemd 单元、文档站骨架、发布产物下载入口。

## 步骤

按上面 1–6 逐项独立分支推进,每项:实现 → 测试/压测 → 文档 → 合入。

## 验收标准

- [x] 慢流场景:单条慢流不拖垮同物理连接的其他流(流控生效)——e2e TestFlowControlNoHOL ✅;
- [x] 热更新:重载期间已建立连接不中断,新连接走新配置——e2e TestReload(坏配置保旧实例、新配置生效新 zone)✅;
- [ ] 双节点:节点 A 的 DNS/路由变更经 etcd 同步到节点 B 生效(EtcdStore 未实现,接口已预留);
- [ ] DNS:records API 增删即时生效(zones API 已实现),DoH/DoT 上游可用(**未做**);
- [ ] 发布产物可一键下载运行(dist 交叉编译产物已就绪,发布托管/下载入口未建)。

## 进度日志

- 2026-09-11:R8-1 ConfigStore(FileStore:原子写/轮询 watch/键逃逸防护)+ Makefile dist 交叉编译矩阵;R8-2 star 每流流控(Window 原语 + MsgWindowUpdate)+ 无头阻塞 e2e,**star 协议自此封盘**;R9 kcp transport 同日合入;
- 2026-09-12:半关闭竞态根治(半关信号经写泵队列 nil 哨兵传递,顺序由队列保证,见下遗留项);R8-2b 配置热更新(Instance.Stop + SIGHUP + Reload 校验,e2e TestReload);R8-3 DNS 迁移垂直切片(handler/dns + APIProvider)+ star 协议文档帧表(0x22/0x30/0x31)同步;
- 2026-10-09:账实核对——交付物状态与验收框同步至实际合入进度;剩余项明确为:**可观测性(#5,未开工)、分发收尾(#6 systemd/文档站/下载入口)、EtcdStore(#3,接口已预留)、DNS 补全(#4 线路匹配/DoT·DoH 上游/UDP 入站/测试)**;四项均不依赖 R5/R6,可先行收尾闭环 M6。

## 遗留项:半关闭 e2e 竞态(2026-09-11 调查记录)

- 现象:`TestProxyHalfCloseEndToEnd` 约 1/6 概率 `reply = ""`(后端应答丢失),已 Skip 隔离。
- 已确认时序:代理上行 EOF → 发 HalfClose → 客户端 CloseWrite 后端 → 后端应答
  → 客户端 pumpUplink 转发——但服务端下行泵在应答帧到达前已排空完毕
  (RemoteClosed 触发的静默排空对"客户端↔后端往返"的额外延迟不够),
  downDone+bothFinished→teardown→mc.Close → 应答帧随后到达被丢弃。
- 已做:下行泵 RemoteClosed 处理后**不退出**(rc 置 nil),CloseWrite 后继续
  转发 Sink 帧,生命周期与 R2 Bridge 状态机对齐;宽限 100ms。
- ✅ **已根治(2026-09-12)**:真根因是客户端 Run 收到半关帧时**立即 CloseWrite 内网连接**,
  而写泵队列中的请求数据尚未刷出——后端收到"空请求+FIN",应答被 EPIPE 链路丢弃
  (此前两轮修复均为时序缓解,未触及此根因)。
- 修复:halfCloseConn 经 connsQ 投递 **nil 哨兵**,写泵排空队列数据后 CloseWrite,
  顺序由队列保证;proxy 侧静默排空 + CloseWrite 后存活转发配合;20 轮压力全过,
  TestProxyHalfCloseEndToEnd 已恢复运行(取消 Skip)。star 协议文档帧表(0x22/0x30/0x31)已随 R8-3 合入同步(2026-09-12)。
