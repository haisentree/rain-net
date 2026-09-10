package star

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// star 线上格式(用于 ctrl 控制通道/隧道):
//
// +---------+--------+----------+----------+----------+--------------+
// | version | type   | reserved | streamID | length   | payload      |
// | 1 byte  | 1 byte | 2 bytes  | 4 bytes  | 4 bytes  | length bytes |
// +---------+--------+----------+----------+----------+--------------+
//
// 整数均为大端序。控制类消息(握手/注册/保活)的 payload 为 JSON,
// 数据类消息(MsgData)的 payload 为原始字节流,不做任何包装。

// ProtocolVersion star 协议版本号,写在每帧的第一个字节
const ProtocolVersion byte = 1

// 消息类型定义
const (
	MsgHandshake         byte = 0x01 // 客户端 -> 服务端 握手认证
	MsgHandshakeAck      byte = 0x02 // 服务端 -> 客户端 握手应答
	MsgRegisterStream    byte = 0x03 // 客户端 -> 服务端 注册流
	MsgRegisterStreamAck byte = 0x04 // 服务端 -> 客户端 注册应答(分配数字流ID)
	MsgConnOpen          byte = 0x05 // 服务端 -> 客户端 请求建立一条到内网服务的连接
	MsgConnOpenAck       byte = 0x06 // 客户端 -> 服务端 连接建立应答
	MsgData              byte = 0x10 // 双向 流数据
	MsgStreamClose       byte = 0x11 // 双向 流关闭
	MsgPing              byte = 0x20 // 双向 保活探测
	MsgPong              byte = 0x21 // 双向 保活应答
)

const (
	// HeaderSize 帧头长度: version(1) + type(1) + reserved(2) + streamID(4) + length(4)
	HeaderSize = 12

	// MaxPayloadSize 单条消息负载上限,防止恶意超大长度声明
	MaxPayloadSize = 1 << 20 // 1MB

	// CtrlStreamID 控制类消息(握手/注册/保活)固定使用的 streamID
	CtrlStreamID uint32 = 0
)

// Message 一条完整的 star 消息
type Message struct {
	Type     byte
	StreamID uint32
	Payload  []byte
}

// WriteMessage 把消息编码为一帧并写入 w,头部和负载合并成一次 Write
func WriteMessage(w io.Writer, m *Message) error {
	if len(m.Payload) > MaxPayloadSize {
		return fmt.Errorf("star: payload size %d exceeds limit %d", len(m.Payload), MaxPayloadSize)
	}

	frame := make([]byte, HeaderSize+len(m.Payload))
	frame[0] = ProtocolVersion
	frame[1] = m.Type
	// frame[2:4] reserved,写 0 留给后续标志位
	binary.BigEndian.PutUint32(frame[4:8], m.StreamID)
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(m.Payload)))
	copy(frame[HeaderSize:], m.Payload)

	_, err := w.Write(frame)
	return err
}

// ReadMessage 从 r 读取一帧
func ReadMessage(r io.Reader) (*Message, error) {
	head := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}

	if head[0] != ProtocolVersion {
		return nil, fmt.Errorf("star: unsupported protocol version %d", head[0])
	}

	m := &Message{
		Type:     head[1],
		StreamID: binary.BigEndian.Uint32(head[4:8]),
	}
	length := binary.BigEndian.Uint32(head[8:12])
	if length > MaxPayloadSize {
		return nil, fmt.Errorf("star: payload size %d exceeds limit %d", length, MaxPayloadSize)
	}
	if length > 0 {
		m.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, m.Payload); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// TypeName 返回消息类型的可读名称,用于日志
func (m *Message) TypeName() string { return MsgTypeName(m.Type) }

// MsgTypeName 返回消息类型的可读名称
func MsgTypeName(t byte) string {
	switch t {
	case MsgHandshake:
		return "handshake"
	case MsgHandshakeAck:
		return "handshakeAck"
	case MsgRegisterStream:
		return "registerStream"
	case MsgRegisterStreamAck:
		return "registerStreamAck"
	case MsgConnOpen:
		return "connOpen"
	case MsgConnOpenAck:
		return "connOpenAck"
	case MsgData:
		return "data"
	case MsgStreamClose:
		return "streamClose"
	case MsgPing:
		return "ping"
	case MsgPong:
		return "pong"
	default:
		return fmt.Sprintf("unknown(0x%02x)", t)
	}
}

// 控制类消息的 JSON 负载定义

// HandshakeReq 握手认证请求 (MsgHandshake)
type HandshakeReq struct {
	Version     int    `json:"version"`
	ClientName  string `json:"clientName,omitempty"`
	KeyPassword string `json:"keyPassword"`
}

// HandshakeAck 握手应答 (MsgHandshakeAck)
type HandshakeAck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// RegisterStreamReq 流注册请求 (MsgRegisterStream)
// StreamId 是配置文件里的字符串流标识,服务端注册成功后分配数字流ID
type RegisterStreamReq struct {
	ClientProxyName string `json:"clientProxyName"`
	StreamId        string `json:"streamId"`
}

// RegisterStreamAck 流注册应答 (MsgRegisterStreamAck)
type RegisterStreamAck struct {
	OK       bool   `json:"ok"`
	StreamId string `json:"streamId"`
	AssignID uint32 `json:"assignId"`
	Message  string `json:"message,omitempty"`
}

// ConnOpenReq 连接建立请求 (MsgConnOpen)
//
// StreamID 是目标服务流的数字ID,ConnID 是服务端为这条外部连接分配的连接ID;
// 建立(connOpenAck ok)之后,该连接的 MsgData/MsgStreamClose 帧的
// streamID 字段都填 ConnID。一条服务流上可以并发多条连接
type ConnOpenReq struct {
	StreamID uint32 `json:"streamId"`
	ConnID   uint32 `json:"connId"`
}

// ConnOpenAck 连接建立应答 (MsgConnOpenAck)
type ConnOpenAck struct {
	ConnID  uint32 `json:"connId"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// SendControl 把控制消息编码为 JSON 后发送,固定使用 CtrlStreamID
func (s *Session) SendControl(msgType byte, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("star: marshal control message %s: %w", MsgTypeName(msgType), err)
	}
	return s.Send(&Message{Type: msgType, StreamID: CtrlStreamID, Payload: payload})
}

// DecodeControl 把控制消息的 JSON 负载解码到 v
func DecodeControl(m *Message, v any) error {
	if len(m.Payload) == 0 {
		return fmt.Errorf("star: message %s has empty payload", m.TypeName())
	}
	return json.Unmarshal(m.Payload, v)
}
