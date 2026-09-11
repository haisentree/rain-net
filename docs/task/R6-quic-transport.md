# R6:QUIC transport(= roadmap M4)

- 状态:pending
- 依赖:R5(复用其连接池接口;顺序已定,见 design.md §十.4)
- 关联:`docs/references.md`(Reverst、hysteria、cloudflared)、`docs/design.md` §二

## 目标

新增 `internal/transport/quic`(基于 quic-go),学习价值最高的一个传输:UDP、TLS1.3 内嵌握手、流状态机、连接迁移(了解即可)。

## 前置学习

- RFC 9000/9001 重点:stream 类型与状态机、流级/连接级流控、TLS1.3 握手内嵌、0-RTT 的安全性权衡;
- quic-go API:`quic.Connection` 的 OpenStream/AcceptStream、`quic.Stream` 的 `CancelRead`(读侧半关闭)、SetDeadline 语义;
- Reverst 源码(整库通读,Go + quic-go 的隧道实践)、cloudflared 的 QUIC 隧道与 http2 fallback。

## 交付物

- `internal/transport/quic/`:Listen(AcceptStream)/ Dial(池化,复用 R5 的池接口);
- 半关闭:CancelRead/CloseWrite 与 rainnet.Conn 语义对齐(QUIC 原生支持,基本零适配);
- TLS 证书配置复用现有 tls transport 的配置形状;
- conformance 全过;压测(quic vs h2 vs tcp,弱网模拟更佳)+ 总结文档 `docs/research/quic-transport.md`。

## 验收标准

- [ ] conformance 六场景通过;
- [ ] 与 Reverst 对照的连接管理(超时/重连/池)有明确取舍记录;
- [ ] 压测 + 总结文档齐备。

## 进度日志

- (无)
