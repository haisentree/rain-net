# R9(可选加餐):kcp transport(= roadmap M7)

- 状态:pending
- 依赖:R1
- 关联:`docs/references.md`(kcp-go/kcptun、Xray mkcp)

## 目标

`internal/transport/kcp`,学习 ARQ、FEC、拥塞控制这一独立知识块;"smux over kcp" 的分层组合方式。

## 交付物

- `internal/transport/kcp/`:kcp-go Listener/Dialer,叠 smux 做多路复用(或接 R5 的池接口);
- 拥塞模式可配置(normal/fast/bbr,学习对比);
- conformance 全过;弱网(丢包/延迟注入)压测:tcp vs quic vs kcp 对比表;
- 总结文档 `docs/research/kcp-transport.md`(ARQ/窗口/拥塞控制学习笔记为主)。

## 验收标准

- [ ] conformance 通过;
- [ ] 弱网对比表(丢包率 × 吞吐/延迟);
- [ ] 总结文档产出。

## 进度日志

- (无)
