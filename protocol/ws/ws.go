// Package ws RFC 6455 WebSocket 的最小实现:握手 + 帧编解码
//
// 支持:文本/二进制消息、续帧分片拼装、ping/pong 自动应答、关闭握手。
// 客户端侧发出的帧按规范必须掩码,服务端侧不掩码。
// 不支持:扩展(RSV 位)、压缩。
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 消息操作码
const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

// websocketGUID RFC 6455 固定的握手 GUID
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// handshakeTimeout 握手阶段的读写超时
const handshakeTimeout = 10 * time.Second

// MaxMessageSize 单条消息上限,防止恶意超大帧
const MaxMessageSize = 1 << 20 // 1MB

// acceptKey 计算 Sec-WebSocket-Accept
func acceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Conn 一条 WebSocket 连接
type Conn struct {
	conn net.Conn
	br   *bufio.Reader
	// client 表示本端是客户端角色:发出的帧必须掩码
	client bool

	writeMu sync.Mutex
	closed  bool
}

// Accept 在服务端已接受的连接上完成升级握手
func Accept(conn net.Conn) (*Conn, error) {
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetDeadline(time.Time{})

	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return nil, fmt.Errorf("ws: read handshake request: %w", err)
	}
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") ||
		!strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("ws: not a websocket upgrade request")
	}
	key := req.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("ws: missing Sec-WebSocket-Key")
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, fmt.Errorf("ws: write handshake response: %w", err)
	}

	// br 里可能已缓冲了后续帧,一并带回
	return &Conn{conn: conn, br: br}, nil
}

// Dial 拨号并完成客户端升级握手,addr 形如 host:port 或 ws://host:port/path
func Dial(addr string, timeout time.Duration) (*Conn, error) {
	host, path := addr, "/"
	if strings.HasPrefix(addr, "ws://") {
		rest := strings.TrimPrefix(addr, "ws://")
		if i := strings.Index(rest, "/"); i >= 0 {
			host, path = rest[:i], rest[i:]
		} else {
			host = rest
		}
	}

	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", host, err)
	}
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetDeadline(time.Time{})

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce)

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: send handshake: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: read handshake response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("ws: handshake status %d", resp.StatusCode)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		conn.Close()
		return nil, errors.New("ws: Sec-WebSocket-Accept mismatch")
	}

	return &Conn{conn: conn, br: br, client: true}, nil
}

// Name 协议名
func (c *Conn) LocalName() string { return "ws" }

// ReadMessage 阻塞读取下一条完整消息
// 自动处理 ping(回 pong)、close(回关闭帧并返回 EOF)、分片拼装
func (c *Conn) ReadMessage() ([]byte, error) {
	var (
		assembling bool
		msg        []byte
	)
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}

		switch op {
		case OpText, OpBinary:
			if fin {
				return payload, nil
			}
			assembling, msg = true, payload
		case OpContinuation:
			if !assembling {
				return nil, errors.New("ws: unexpected continuation frame")
			}
			msg = append(msg, payload...)
			if fin {
				return msg, nil
			}
		case OpPing:
			if err := c.writeControl(OpPong, payload); err != nil {
				return nil, err
			}
		case OpPong:
			// 忽略,当前不做主动 ping
		case OpClose:
			_ = c.writeControl(OpClose, nil)
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("ws: unknown opcode 0x%02x", op)
		}
	}
}

// WriteMessage 以二进制帧写出一条消息,可并发调用
func (c *Conn) WriteMessage(payload []byte) error {
	return c.writeFrame(true, OpBinary, payload)
}

// Close 发送关闭帧并关闭底层连接
func (c *Conn) Close() error {
	c.writeMu.Lock()
	closed := c.closed
	c.closed = true
	c.writeMu.Unlock()
	if !closed {
		_ = c.writeFrame(true, OpClose, nil)
	}
	return c.conn.Close()
}

// readFrame 读取一帧,自动解掩码
func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	if head[0]&0x70 != 0 {
		err = errors.New("ws: reserved bits set(扩展不支持)")
		return
	}
	opcode = head[0] & 0x0f
	masked := head[1]&0x80 != 0

	length := uint64(head[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > uint64(MaxMessageSize) {
		err = fmt.Errorf("ws: frame too large %d", length)
		return
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, maskKey[:]); err != nil {
			return
		}
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return
}

// writeFrame 写出一帧:客户端角色必须掩码
func (c *Conn) writeFrame(fin bool, opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("ws: connection closed")
	}

	// 头部最长:2 + 8(64位长度) + 4(掩码键) = 14 字节
	var head [14]byte
	head[0] = opcode
	if fin {
		head[0] |= 0x80
	}

	n := len(payload)
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	switch {
	case n < 126:
		head[1] = maskBit | byte(n)
	case n <= 0xffff:
		head[1] = maskBit | 126
		binary.BigEndian.PutUint16(head[2:4], uint16(n))
	default:
		head[1] = maskBit | 127
		binary.BigEndian.PutUint64(head[2:10], uint64(n))
	}
	headLen := 2
	if head[1]&0x7f == 126 {
		headLen = 4
	} else if head[1]&0x7f == 127 {
		headLen = 10
	}

	var maskKey [4]byte
	if c.client {
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		copy(head[headLen:headLen+4], maskKey[:])
		headLen += 4
	}

	// 头+负载合并为一次写,避免半帧
	frame := make([]byte, 0, headLen+n)
	frame = append(frame, head[:headLen]...)
	if c.client {
		masked := make([]byte, n)
		for i := 0; i < n; i++ {
			masked[i] = payload[i] ^ maskKey[i%4]
		}
		frame = append(frame, masked...)
	} else {
		frame = append(frame, payload...)
	}

	_, err := c.conn.Write(frame)
	return err
}

// writeControl 写控制帧(短小,无分片)
func (c *Conn) writeControl(opcode byte, payload []byte) error {
	return c.writeFrame(true, opcode, payload)
}
