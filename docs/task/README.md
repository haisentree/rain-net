# 长任务队列(Task Queue)

> 跨会话执行的重构与里程碑任务。来源:`docs/design.md` §八 执行顺序 + §九 定稿细则、`docs/roadmap.md` 里程碑。
> **新会话接入方式**:先读本文件 → 找最靠前的 `pending`/`in-progress` 任务 → 打开对应文件按"步骤"执行 → 完成一条勾一条 → 全部完成后更新本文件状态表与任务文件的状态栏。

## 使用约定

- 状态:`pending`(未开始)/ `in-progress`(进行中)/ `done`(完成)/ `dropped`(放弃,注明原因);
- 每个 R 任务一个 git 分支(`r0-e2e`、`r1-core` …),**e2e 全绿才允许合入**;
- 任务文件内"进度日志"为追加式,记日期 + 一句话,不删旧记录;
- schema/协议/命名的任何变更,同步更新 `docs/design.md` 与 `etc/` 示例,否则不合入;
- 完成任务后回到本文件把状态表改掉,再领下一个。

## 任务总览

| 编号 | 文件 | 目标 | 依赖 | 状态 |
|------|------|------|------|------|
| R0 | [R0-e2e-safety-net.md](R0-e2e-safety-net.md) | 迁移 e2e 到 test/e2e,建立重构安全网 | — | **done**(2026-09-11) |
| R1 | [R1-core-abstraction.md](R1-core-abstraction.md) | pkg/rainnet + Transport 抽象 + 一致性套件(=M1) | R0 | **done**(2026-09-11) |
| R2 | [R2-router-bridge.md](R2-router-bridge.md) | Router 迁移 + Bridge 半关闭契约 + 基础装饰器 | R1 | **done**(2026-09-11) |
| R3 | [R3-star-split.md](R3-star-split.md) | star 拆解 + 新配置 schema + admin 聚合 + 协议半关帧 | R2 | **done**(2026-09-11,dialer/star 推迟至 R7) |
| R4 | [R4-ws-transport.md](R4-ws-transport.md) | websocket transport(=M2) | R1 | **done**(2026-09-11) |
| R5 | [R5-h2-transport.md](R5-h2-transport.md) | http2 h2c transport(=M3) | R1 | pending |
| R6 | [R6-quic-transport.md](R6-quic-transport.md) | QUIC transport(=M4) | R1 | pending |
| R7 | [R7-bridge-conversion.md](R7-bridge-conversion.md) | 传输转换矩阵全量 + 嗅探 ingress(=M5) | R2,R4–R6 | **done**(2026-09-11) |
| R8 | [R8-productization.md](R8-productization.md) | 流控/热更新/ConfigStore/DNS 迁移/可观测(=M6) | R3,R7 | **in-progress**(流控+ConfigStore+Makefile ✅;剩热更新/DNS/可观测) |
| R9 | [R9-kcp-optional.md](R9-kcp-optional.md) | kcp transport(加餐,=M7) | R1 | **done**(2026-09-11) |

## 执行顺序建议

主线:R0 → R1 → R2 → R3(重构闭环,此后模型落地)→ R4 → R5 → R6(顺序已定:ws → h2 → quic,见 design.md §十.4)→ R7 → R8。
R9 为可选加餐。R4–R6 每个都必须:过 conformance 套件 + 压测 + 产出总结文档后才算 done。
