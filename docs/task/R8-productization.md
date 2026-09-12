# R8:产品化收尾(= roadmap M6)

- 状态:in-progress(流控 ✅ + ConfigStore/Makefile ✅ 已合入;剩热更新/DNS 迁移/可观测,2026-09-11)
- 依赖:R3、R7
- 关联:`docs/design.md` §七(控制面)、§九.3(取消链);`docs/research/nps.md` §2.4(功能清单);roadmap M6

## 目标

从"能用的重构成果"到"成熟产品形态":流控、热更新、配置持久化与多节点同步、DNS 迁移、可观测性、分发。

## 交付物(可拆分独立推进)

1. **star 按流流控/背压** ✅(2026-09-11):Window 原语 + MsgWindowUpdate 双向流控;客户端每连接写泵消除跨流头阻塞;e2e TestFlowControlNoHOL(慢流占满窗口、快流不受影响);proxy 下行排空竞态修复(静默窗口)。**star 协议自此封盘**(协议文档帧表待同步 0x22);
2. **配置热更新**:实例替换式重载(Caddy 路线),依赖 §九.3 取消链排空旧连接;admin API 触发;
3. **ConfigStore** ✅(2026-09-11,部分):internal/configstore 接口 + FileStore(原子写/轮询 watch/键逃逸防护)+ 测试;EtcdStore 待配置中心落地(接口已预留);
4. **DNS 迁移**:`internal/handler/dns` + dns ingress(udp/tcp transport);缓存命中不进 Bridge;线路匹配(地理/运营商);`/api/dns/*/zones` 外置管理;上游 dialer 化 → DoT/DoH = tls/http transport 出口;
5. **可观测性**:metrics(错误三分类、每 transport 连接数/流量)、日志贯穿 ConnID、admin 大盘补充;
6. **分发**:单二进制构建(linux/amd64/arm64 + darwin + windows)、最小 systemd 单元、文档站骨架。

## 步骤

按上面 1–6 逐项独立分支推进,每项:实现 → 测试/压测 → 文档 → 合入。

## 验收标准

- [ ] 慢流场景:单条慢流不拖垮同物理连接的其他流(流控生效);
- [ ] 热更新:重载期间已建立连接不中断,新连接走新配置;
- [ ] 双节点:节点 A 的 DNS/路由变更经 etcd 同步到节点 B 生效;
- [ ] DNS:records API 增删即时生效,DoH/DoT 上游可用;
- [ ] 发布产物可一键下载运行。

## 进度日志

- (无)

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
  TestProxyHalfCloseEndToEnd 已恢复运行(取消 Skip)。star 协议文档帧表(0x22/0x30/0x31)同步待办。
