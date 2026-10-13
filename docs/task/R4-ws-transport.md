# R4:websocket transport(= roadmap M2)

- 状态:**done**(2026-09-11,commit 588f3b1)
- 依赖:R1(conformance 套件)
- 关联:`docs/references.md`(B 类:chisel、wstunnel)、`docs/design.md` §二 半关闭矩阵、`protocol/ws/`(现有基础)

## 目标

新增 `internal/transport/ws`:ws 作为传输层承载任意流,定义帧→流映射与半关闭降级策略。第一个"学习型传输"样板:RFC 阅读 → 实现 → conformance → 压测 → 总结文档。

## 前置学习(做之前先看)

- RFC 6455 重点:帧格式(binary/text)、masking、close frame 语义(整条连接级,无半关闭);
- `protocol/ws/` 现有实现(gobwas/ws 已在依赖);
- chisel 的 ws 承载方案、wstunnel 的帧设计(见 references.md §B)。

## 交付物

- `internal/transport/ws/`:Listen/Dial 实现;binary 帧承载字节流、帧大小上限、ping/pong 保活与超时;
- **半关闭降级决策**(design.md 矩阵的 ws 列):CloseWrite → 记"已关写"标记;EOF → 排空后整条关闭;决策写入 design.md 并实现;
- Dial 侧连接复用策略(单 ws 连接单流起步,复用留到 R7 一并统一);
- conformance 全过(注意半关闭场景按 ws 降级语义断言);
- 压测:ws vs tcp 吞吐/延迟对比;总结文档 `docs/research/ws-transport.md`。

## 验收标准

- [x] conformance 六场景通过(半关闭按降级语义)——test/conformance/ws_test.go;
- [x] 压测数据 + 总结文档产出——docs/research/ws-transport.md;
- [x] design.md 半关闭矩阵 ws 行与实现一致。

## 进度日志

- 2026-09-11:合入 588f3b1(internal/transport/ws);conformance 套件 ws_test.go 覆盖;总结文档 docs/research/ws-transport.md;design.md 半关闭矩阵 ws 行为降级契约。2026-10-09 补记状态(账实核对)。
