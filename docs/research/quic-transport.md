# R6 总结:QUIC transport

> 2026-09-11,R6(M4)交付。实现:`internal/transport/quic/`(quic-go v0.62);
> conformance 六场景全过(半关闭原生支持,完整断言);基准:bench_transport_test.go。

## 实现要点

1. **连接池(与 h2 同构)**:Transport 实例持有共享 `quic.Transport`(单一本地 UDP socket),
   Dial 按 (地址) 复用池中的 `quic.Conn`,每次开新流;并发拨号去重时关冗余连接、返回池中已有连接
   (第一版这里关错了连接——把调用方要用的关掉,复用逻辑写反,由并发场景测试逮住)。
2. **半关闭原生对齐**:`CloseWrite` = `stream.Close()`(发送 FIN,读方向继续),
   `Close` = Close + `CancelRead`(整条终止)。conformance 六场景完整断言,无需降级。
3. **流控免费**:QUIC 内建连接级/流级窗口;接收窗口放大至 4MB(流)/16MB(连接)——
   quic-go v0.61+ 的"未读数据超 2 倍窗口即断连"硬保护(208KiB 默认)在代理回环场景会误伤,
   放大窗口即提高保护阈值(内存换吞吐,代理场景合理)。
4. **加密内嵌**:TLS1.3 握手内嵌 QUIC,证书配置复用 transport.Options(自签名 → SkipVerify)。
5. **多路复用**:Listen 侧每 QUIC 连接一个 streamsOf goroutine,每条新流 = 一条逻辑连接。

## 踩坑记录(quic-go v0.62 API)

- `Connection` 已改名 `Conn`(具体类型,非接口,不可比较 → 池条目存指针);
- `Listen/Dial` 需要显式 `net.PacketConn`(客户端推荐共享 `quic.Transport`);
- `Transport.Dial` 无本地地址参数(共享 socket 即本地端)。

## 基准(1MB 回环,benchtime=20x,并发读写)

| 传输 | 吞吐 | 备注 |
|------|------|------|
| tcp | ~4.5 GB/s | 基线 |
| tls | ~859 MB/s | |
| ws | ~629 MB/s | |
| h2 | ~267 MB/s | |
| quic | ~166 MB/s | 用户态 UDP 栈 + 拥塞控制预热;窗口调优/0-RTT 为远期优化 |

注意:QUIC 基准对"写满才读"模式敏感(未读保护),基准已改为并发读写以镜像真实 Bridge 行为。

## 已知取舍

- 弱网优势(丢包恢复不阻塞其他流)未在本地回环基准中体现(回环无丢包);
- 0-RTT、连接迁移、datagram 未启用(roadmap 远期)。
