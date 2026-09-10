# star 协议线上格式

star 协议用于 star 服务端与客户端（dailer）之间的 **ctrl 控制通道/隧道**。设计目标：

1. 在一条 TCP 连接上承载多条逻辑流（为多路复用打基础）
2. 握手认证（`keyPassword`），与配置文件中 `dailerList.keyPassword` 对应
3. 流注册：客户端把内网服务注册到服务端，与配置中 `clientProxyName`/`streamId` 对应

代码位置：`protocol/star/`（`message.go` 帧格式与控制消息定义、`session.go` 会话收发、`handshake.go` 握手与流注册流程）。

## 帧格式

每条消息 = 固定 12 字节帧头 + 负载，整数均为大端序：

```
+---------+--------+----------+----------+----------+--------------+
| version | type   | reserved | streamID | length   | payload      |
| 1 byte  | 1 byte | 2 bytes  | 4 bytes  | 4 bytes  | length bytes |
+---------+--------+----------+----------+----------+--------------+
```

| 字段 | 说明 |
|------|------|
| version | 协议版本，当前为 `0x01`，不匹配直接拒绝 |
| type | 消息类型，见下表 |
| reserved | 保留，当前写 0，读时忽略（留给后续标志位） |
| streamID | 数字流 ID；控制类消息固定为 0 |
| length | 负载长度，上限 1MB（`MaxPayloadSize`），防止恶意超大长度声明 |

## 消息类型

| 值 | 名称 | 方向 | 负载 |
|-----|------|------|------|
| 0x01 | handshake | 客户端 → 服务端 | JSON `HandshakeReq` |
| 0x02 | handshakeAck | 服务端 → 客户端 | JSON `HandshakeAck` |
| 0x03 | registerStream | 客户端 → 服务端 | JSON `RegisterStreamReq` |
| 0x04 | registerStreamAck | 服务端 → 客户端 | JSON `RegisterStreamAck` |
| 0x05 | connOpen | 服务端 → 客户端 | JSON `ConnOpenReq` |
| 0x06 | connOpenAck | 客户端 → 服务端 | JSON `ConnOpenAck` |
| 0x10 | data | 双向 | 原始字节流，不包装 |
| 0x11 | streamClose | 双向 | 空（保留扩展位） |
| 0x20 | ping | 双向 | 空（保留可作 opaque token） |
| 0x21 | pong | 双向 | 与 ping 负载一致 |

约定：控制类消息负载用 JSON（可读、好调试），数据类负载保持原始字节（零拷贝转发）。

### 流（stream）与连接（conn）

- **流**：客户端注册的内网服务，数字流ID由服务端分配。握手、注册等控制消息用 `streamID=0`。
- **连接**：外部用户的一次 TCP 连接。服务端收到外部连接后，通过 `connOpen` 在目标流上建立连接并分配**连接ID**；建立成功后，这条连接的 `data`/`streamClose` 帧的 streamID 字段填**连接ID**。
- 一条流上可以并发任意多条连接，互不干扰。

## 控制消息定义

```go
type HandshakeReq struct {
    Version     int    `json:"version"`
    ClientName  string `json:"clientName,omitempty"`
    KeyPassword string `json:"keyPassword"`
}

type HandshakeAck struct {
    OK      bool   `json:"ok"`
    Message string `json:"message,omitempty"`
}

type RegisterStreamReq struct {
    ClientProxyName string `json:"clientProxyName"`
    StreamId        string `json:"streamId"`
}

type RegisterStreamAck struct {
    OK       bool   `json:"ok"`
    StreamId string `json:"streamId"`
    AssignID uint32 `json:"assignId"`
    Message  string `json:"message,omitempty"`
}

type ConnOpenReq struct {
    StreamID uint32 `json:"streamId"` // 目标服务流的数字ID
    ConnID   uint32 `json:"connId"`   // 服务端为本连接分配的连接ID
}

type ConnOpenAck struct {
    ConnID  uint32 `json:"connId"`
    OK      bool   `json:"ok"`
    Message string `json:"message,omitempty"`
}
```

## 交互流程（反向代理/端口映射）

```
外部用户          服务端(proxy监听+ctrlproxy)         客户端(dailer)         内网服务
   |                     |                              |                    |
   |                     |<--- handshake(keyPassword) --|                    |
   |                     |---- handshakeAck(ok) ------->|                    |
   |                     |<--- registerStream ----------|                    |
   |                     |---- registerStreamAck(id=N)->|                    |
   |                     |                              |                    |
   |-- TCP 连接 -------->|                              |                    |
   |                     |---- connOpen(N, connID=M) -->|                    |
   |                     |                              |-- 拨内网服务 ----->|
   |                     |<--- connOpenAck(M, ok) ------|                    |
   |                     |<---- data(M, bytes) =========|=  上行             |
   |<== data(M, bytes) ==|                              |                    |
   |== data(M, bytes) ==>|---- data(M, bytes) --------->|---> 写入内网服务   |
   |                     |                              |                    |
```

- 多条外部连接并发时各自有独立 connID，在同一条 ctrl 连接上多路复用。
- **关闭语义**：任一侧关闭都会以 `streamClose(connID)` 通知对端。客户端侧后端关闭时（如 HTTP/1.0 服务响应完即断开），该帧表示"我不再上行"，服务端按序**排空在途下行数据后**才关闭外部连接（半关闭），避免响应字节被丢弃；服务端侧主动关闭时对端直接终止连接。
- 客户端断线时服务端清理其名下所有流和连接。
- 路由规则：服务端配置的 `connect` 条目（`proxyName`/`clientProxyName`/`streamId`）决定外部连接落到哪条流，多条目标间 round-robin；某条流下线后自动切换到其余目标（故障转移）。

设计决策：**数字流ID由服务端统一分配**（客户端只上报配置里的字符串 `streamId`）。多客户端接入同一服务端时由服务端流表保证不冲突，也符合"代理配置集中在服务端"的产品思路。

## 多客户端认证与身份绑定

服务端的 clientProxy 清单同时是客户端凭据表：每个 `clientProxy.keyPassword` 对应一个客户端身份。

- 握手密码命中某个 `clientProxy.keyPassword` → 该会话绑定为此 `clientProxyName`，**只能注册自己名下的流**，越权注册会被拒绝；
- 命中 `settings.keyPassword` → 管理员身份，可注册任意 `clientProxyName`；
- 多个客户端并存时，`connect` 路由规则在多条流之间 round-robin，某个客户端下线后流量自动切换到其余客户端（故障转移）。

## 传输加密（TLS）

TLS 只作用于传输层，协议帧格式不变：

- 服务端：listener `settings.tls: true` + `certFile`/`keyFile`，监听器即变为 TLS；
- 客户端：dailer `tls: true`（自签名证书配 `tlsSkipVerify: true` 或改用受信 CA）。

ctrl 通道加密后，隧道内转发的一切流量（数据帧）随之加密；面向外部用户的 proxy 监听器按服务形态决定是否 TLS（如 frp 的 http vhost 口保持明文）。

## 使用约定

- `Session.Receive` 只能由一个读循环 goroutine 串行调用；`Send` 有写锁，可并发。
- 服务端读循环收到消息后按 type 分发；`ServerRegisterStream` 只负责回 ack，**不再读连接**（消息读取统一在分发循环）。
- `ClientHandshake` 只有网络/格式错误才返回 error，密码是否正确检查 `ack.OK`；服务端 `ServerHandshake` 认证失败会自动回 nack 并返回 error。
- `Session.Ping` 是同步辅助方法，内部会调 `Receive`，只适用于还没有独立读循环的简单场景；接入 ctrlproxy/dailer 后应由读循环处理 ping/pong。
- 握手/注册阶段有 10 秒读写超时，防止客户端挂着连接不走握手。

最小示例：

```go
// 客户端
conn, _ := net.Dial("tcp", "server:5172")
sess := star.NewSession(conn)
ack, _ := star.ClientHandshake(sess, star.HandshakeReq{KeyPassword: "xxx"})
rack, _ := star.ClientRegisterStream(sess, star.RegisterStreamReq{
    ClientProxyName: "clientProxy-0", StreamId: "stream-0"})
sess.Send(&star.Message{Type: star.MsgData, StreamID: rack.AssignID, Payload: data})

// 服务端（读循环内）
msg, _ := sess.Receive()
switch msg.Type {
case star.MsgRegisterStream:
    star.ServerRegisterStream(sess, msg, func(req star.RegisterStreamReq) (uint32, error) {
        return streamTable.Assign(req) // 从流表分配
    })
}
```

## 后续演进（预留）

- reserved 字段：压缩/加密标志、分片标志
- TLS：隧道加密，监听器 transport 层替换，协议本身不变
- 流量控制：按 streamID 的窗口/背压（`MsgStreamClose` 负载可扩展原因码）
- 服务端主动下发配置（对应配置文件中"后期单独开启一个流,进行控制和下发配置信息"的注释）
