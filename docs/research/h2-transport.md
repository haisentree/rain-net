# R5 总结:http2 h2c transport

> 2026-09-11,R5(M3)交付。实现:`internal/transport/h2/`(golang.org/x/net/http2);
> conformance 全场景过(半关闭由拨号侧断言);基准:bench_transport_test.go。

## 实现要点

1. **流 = 连接**:每个 HTTP/2 请求即一条逻辑连接。服务端逐 TCP 连接跑
   `http2.Server.ServeConn`(纯 h2c,不经 HTTP/1);拨号侧 POST 请求体管道 = 上行,响应体 = 下行。
2. **连接复用免实现**:单个 Transport 实例持有共享 `http2.Client`,同目标 Dial 自动复用
   物理连接——http2 协议内建多路复用,连接池"零代码"(R6 QUIC 池接口的参照形态)。
3. **半关闭**:客户端角色 `CloseWrite` = 关闭请求体管道(END_STREAM),服务端读到请求体 EOF;
   服务端角色不支持(应答结束即流结束)——半关闭矩阵按拨号侧断言,conformance 全场景通过。
4. **流控免费**:h2 内建连接级/流级窗口,Write 在窗口耗尽时自然阻塞;服务端每次 Read/Write 后
   Flush 保证时序。
5. **deadline**:客户端读侧经常驻读泵(16KB × 64 帧队列)+ 定时器实现,错误实现 `Timeout()`
   语义(os.IsTimeout 可识别);写侧 v1 不生效(流控背压)。
6. **伪装**:path 可配置(默认 "/"),与反代共存。

## 踩坑记录

1. **响应头不主动发**:handler 里不 WriteHeader+Flush,拨号侧 `client.Do` 会一直等头——
   连接"建立"必须显式 Flush 响应头。
2. **Dial ctx 生命周期**:`http.NewRequestWithContext(dialCtx, ...)` 会把流生命周期绑到
   dial ctx 上——调用方 `defer cancel()` 后流立即死(并发场景首爆)。修复:Dial 内用
   独立 ctx + 看门狗,建立阶段受 ctx 约束,建立后生命周期只归 Close(与 net.Dialer 同语义)。

## 基准(1MB 回环,benchtime=20x)

| 传输 | 吞吐 | 备注 |
|------|------|------|
| tcp | ~5.8 GB/s | 基线 |
| tls | ~678 MB/s | |
| ws | ~633 MB/s | |
| h2 | ~353 MB/s | 每次 Write 一次 Flush;批量化(攒帧再 Flush)可显著提升,远期优化 |

## 已知取舍

- 服务端角色无半关闭(HTTP 语义决定);
- 流级 deadline 不支持(读泵粒度),会话级经 SetReadDeadline 近似;
- 读队列 64×16KB 上限,慢读时 h2 流控反压(无内存膨胀风险)。
