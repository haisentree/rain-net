# R5:http2 h2c transport(= roadmap M3)

- 状态:pending
- 依赖:R1
- 关联:`docs/references.md`(sing-box v2ray-transport、Xray h2)、`docs/design.md` §二

## 目标

新增 `internal/transport/h2`:以 h2c(明文 http2,可叠 tls transport)承载流。学习点:HPACK、流级流控、多路复用。

## 前置学习

- RFC 9113(http2)重点:stream 状态机、END_STREAM(半关闭)、flow control window;
- golang.org/x/net/http2 的 client transport 与 server 的裸 h2c 用法;
- sing-box/Xray 的 h2 transport 配置形状(path/host 伪装字段)。

## 交付物

- `internal/transport/h2/`:Listen(服务端 accept stream)/ Dial(客户端开 stream);
- **连接池**:一条物理连接多路 stream,Dial 从池取/建——第一个实现"池化 Dial"语义的传输,池接口形状要为 R6(quic)复用而设计;
- path/host 可配置(伪装与反代共存场景);
- 半关闭:END_STREAM ↔ CloseWrite 映射;
- conformance 全过(多流并发场景重点);压测 + 总结文档 `docs/research/h2-transport.md`。

## 验收标准

- [ ] 多流并发场景:慢流不阻塞其他流(流级窗口生效);
- [ ] 连接池:并发 Dial 不重复建物理连接;
- [ ] conformance + 压测 + 总结文档齐备。

## 进度日志

- (无)
