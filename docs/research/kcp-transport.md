# R9 总结:kcp transport(加餐)

> 2026-09-11,R9(M7 加餐)交付。实现:`internal/transport/kcp/`(xtaci/kcp-go + xtaci/smux);
> conformance 六场景全过(半关闭走降级断言);基准:bench_transport_test.go。

## 实现要点

1. **分层组合**(kcptun 同款):KCP(ARQ/FEC/拥塞控制,UDP 之上)→ smux(多路复用)→ 业务流。
   smux 的 Stream 原生实现 net.Conn,直接输出。
2. **连接池**:Dial 按地址复用 smux 会话(死会话自动重建),与 quic/h2 的池接口同构;
   并发拨号去重:冗余新建的会话关闭、复用已有。
3. **Listen 形态**:KCP 监听 → 每连接升级 smux.Server → 会话内 AcceptStream → facade listener。
4. **半关闭**:smux 流仅整条 Close,无半关闭 → conformance 走 `Caps{HalfClose:false}` 降级场景。

## 学习笔记:KCP 协议栈(RFC 视角)

- **ARQ**:选择性重传 + 快速重传(跳过确认即重发),对比 TCP 的 RTO 退避——牺牲带宽换低延迟;
- **FEC**(可选):前向纠错,群体丢包场景减少重传;
- **拥塞控制**:normal(fc)/fast(srtt 平滑)/bbr 三模式可配,KCP 默认偏激进——弱网下吞吐优势明显,公平性差于 TCP;
- 本机回环基准无法体现弱网优势(无丢包),结论以"协议机制学习"为主:弱网(丢包率 × 延迟)场景 KCP 的设计目标是延迟敏感型传输。

## 基准(1MB 回环,benchtime=10x)

| 传输 | 吞吐 | 备注 |
|------|------|------|
| tcp | ~4.5-5.8 GB/s | 基线(splice) |
| tls | ~760-860 MB/s | |
| ws | ~630-680 MB/s | |
| quic | ~166 MB/s | 拥塞控制预热敏感 |
| h2 | ~270-350 MB/s | Flush 粒度主导 |
| kcp | (回环数据偏低,KCP 定时器粒度 10-40ms 主导) | 弱网场景才是主场 |

## 已知取舍

- smux 流无半关闭 → 半关闭矩阵降级列;
- KCP 定时器粒度导致回环小包延迟偏高(设计目标是弱网,非回环);
- 未暴露 FEC/模式配置(远期经 transport.Options 扩展)。
