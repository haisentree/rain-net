# R4 总结:websocket transport

> 2026-09-11,R4(M2)交付。实现:`internal/transport/ws/`(gobwas/ws);
> 一致性:`test/conformance`(六场景,ws 走降级断言);基准:`test/conformance/bench*_test.go`。

## 实现要点

1. **帧→流映射**:写侧每次 `Write` 一个 binary 帧(调用方分块,如 io.Copy 64KB;超过 1MB 自动分帧);
   读侧逐帧读取按序拼接,消息边界不保留。单帧上限 1MB,超限拒绝(防恶意声明)。
2. **控制帧**:ping 自动回 pong;close 视为 EOF;未知 opcode 报错。
3. **掩码**:客户端→服务端帧必须掩码——server 角色读帧后 `ws.Cipher` 解掩码,client 角色写出帧经
   `wsutil.WriteClientMessage` 加掩码;server 角色写出用 `wsutil.WriteServerMessage`。
4. **半关闭**:ws 协议无半关闭,`rainnet.Conn.CloseWrite` 经适配返回 `ErrNotSupported`,
   转换链降级为整条关闭(conformance 以 `Caps{HalfClose:false}` 断言降级场景)。
5. **Listen 形态**:真实 TCP 监听 + http.Server 升级(路径可配,默认 "/"),
   升级成功后的连接经 channel 交给 facade listener——Accept 出的即标准 net.Conn。
6. **坑**:gobwas `ws.Dial` 在无缓冲余数据时返回 **nil bufio.Reader**(必须兜底);
   facade listener 的 Addr 必须取真实 listener 的地址(否则 :0 永远拨不通);
   `srv.Serve` 必须跑在真实 listener 上。

## 基准(1MB 回环,benchtime=20x,本机 linux/amd64)

| 传输 | 吞吐 | 备注 |
|------|------|------|
| tcp | ~4.1 GB/s | 基线,io.Copy 走 splice |
| tls | ~762 MB/s | 加解密开销主导 |
| ws | ~622 MB/s | 帧编解码 + 掩码 + HTTP 层,较 tls 再降 ~18% |

结论:ws 传输开销与 tls 同量级,作为可替换传输完全可用;帧大小与 io.Copy 块对齐时性能最佳。

## 已知取舍

- 消息边界不保留:ws 帧在流语义下只是搬运单元,对端不感知边界;
- 会话级背压沿用 net.Conn 语义(帧队列无窗口,依赖 TCP 流控);
- 未做 permessage-deflate 压缩(远期可加,reserved 扩展位思路同 star)。
