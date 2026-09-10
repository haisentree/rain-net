package star

import (
	"fmt"
	"time"
)

// 握手与流注册的超时时间
const (
	handshakeTimeout = 10 * time.Second
	registerTimeout  = 10 * time.Second
)

// ClientHandshake 客户端发起握手认证,阻塞等待服务端应答
//
// 仅网络/格式错误返回 error;密码是否正确需检查返回的 ack.OK
func ClientHandshake(s *Session, req HandshakeReq) (*HandshakeAck, error) {
	s.deadline(handshakeTimeout)
	defer s.clearDeadline()

	if req.Version == 0 {
		req.Version = int(ProtocolVersion)
	}

	if err := s.SendControl(MsgHandshake, req); err != nil {
		return nil, fmt.Errorf("star: send handshake: %w", err)
	}

	msg, err := s.Receive()
	if err != nil {
		return nil, fmt.Errorf("star: read handshake ack: %w", err)
	}
	if msg.Type != MsgHandshakeAck {
		return nil, fmt.Errorf("star: unexpected message %s while waiting handshake ack", msg.TypeName())
	}

	var ack HandshakeAck
	if err := DecodeControl(msg, &ack); err != nil {
		return nil, err
	}
	return &ack, nil
}

// ServerHandshake 服务端等待握手并校验 keyPassword
//
// 认证失败时回复 OK=false 的应答并返回错误,由调用方决定是否关闭连接
func ServerHandshake(s *Session, keyPassword string) (*HandshakeReq, error) {
	return ServerHandshakeVerify(s, func(req HandshakeReq) bool {
		return req.KeyPassword == keyPassword
	})
}

// ServerHandshakeVerify 服务端等待握手,由 verify 自定义校验逻辑
// (例如多凭据场景:按密码反查客户端身份)
// 认证失败时回复 OK=false 的应答并返回错误
func ServerHandshakeVerify(s *Session, verify func(HandshakeReq) bool) (*HandshakeReq, error) {
	s.deadline(handshakeTimeout)
	defer s.clearDeadline()

	msg, err := s.Receive()
	if err != nil {
		return nil, fmt.Errorf("star: read handshake: %w", err)
	}
	if msg.Type != MsgHandshake {
		return nil, fmt.Errorf("star: unexpected message %s while waiting handshake", msg.TypeName())
	}

	var req HandshakeReq
	if err := DecodeControl(msg, &req); err != nil {
		return nil, err
	}

	ack := HandshakeAck{OK: true}
	if verify == nil || !verify(req) {
		ack = HandshakeAck{OK: false, Message: "invalid key password"}
	}
	if err := s.SendControl(MsgHandshakeAck, ack); err != nil {
		return nil, fmt.Errorf("star: send handshake ack: %w", err)
	}
	if !ack.OK {
		return nil, fmt.Errorf("star: handshake rejected: %s", ack.Message)
	}
	return &req, nil
}

// ClientRegisterStream 客户端注册一条流,阻塞等待服务端分配数字流ID
func ClientRegisterStream(s *Session, req RegisterStreamReq) (*RegisterStreamAck, error) {
	s.deadline(registerTimeout)
	defer s.clearDeadline()

	if err := s.SendControl(MsgRegisterStream, req); err != nil {
		return nil, fmt.Errorf("star: send register stream: %w", err)
	}

	msg, err := s.Receive()
	if err != nil {
		return nil, fmt.Errorf("star: read register stream ack: %w", err)
	}
	if msg.Type != MsgRegisterStreamAck {
		return nil, fmt.Errorf("star: unexpected message %s while waiting register stream ack", msg.TypeName())
	}

	var ack RegisterStreamAck
	if err := DecodeControl(msg, &ack); err != nil {
		return nil, err
	}
	return &ack, nil
}

// AssignStreamFunc 服务端为一条注册请求分配数字流ID
// 返回 error 时拒绝注册
type AssignStreamFunc func(req RegisterStreamReq) (uint32, error)

// ServerRegisterStream 服务端处理一条已读取的流注册消息,assignID 由服务端的流表提供
//
// msg 必须是读循环分发出来的 MsgRegisterStream;注册失败时回复 OK=false
// 的应答并返回错误。注意本函数只回 ack,不再从连接读取消息,
// 消息的读取统一由调用方的读循环完成
func ServerRegisterStream(s *Session, msg *Message, assignID AssignStreamFunc) (*RegisterStreamAck, error) {
	if msg.Type != MsgRegisterStream {
		return nil, fmt.Errorf("star: unexpected message %s, want registerStream", msg.TypeName())
	}

	var req RegisterStreamReq
	if err := DecodeControl(msg, &req); err != nil {
		return nil, err
	}

	ack := RegisterStreamAck{OK: true, StreamId: req.StreamId}
	if assignID != nil {
		id, err := assignID(req)
		if err != nil {
			ack = RegisterStreamAck{OK: false, StreamId: req.StreamId, Message: err.Error()}
		} else {
			ack.AssignID = id
		}
	}
	if err := s.SendControl(MsgRegisterStreamAck, ack); err != nil {
		return nil, fmt.Errorf("star: send register stream ack: %w", err)
	}
	if !ack.OK {
		return nil, fmt.Errorf("star: register stream %q rejected: %s", req.StreamId, ack.Message)
	}
	return &ack, nil
}
