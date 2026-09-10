// Package codec 协议转化层:把各种入站/出站协议适配为统一的双向消息流
//
// 转化模型:listener(协议A) → Codec.Accept → MessageConn →(桥接/隧道)→
// Codec.Dial → MessageConn → target(协议B)。
// raw 协议没有消息边界,ReadMessage 返回到达的分块;
// ws 等消息协议则保留消息边界。
package codec

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// MaxMessageSize 单条消息上限,防止恶意超大帧
const MaxMessageSize = 1 << 20 // 1MB

// MessageConn 双向消息流
type MessageConn interface {
	// ReadMessage 阻塞读取一条消息;对端关闭返回 io.EOF
	ReadMessage() ([]byte, error)
	// WriteMessage 写出一条消息,可并发调用
	WriteMessage(payload []byte) error
	// Close 关闭底层连接
	Close() error
}

// Codec 协议适配器:服务端入站(Accept)与客户端出站(Dial)两个方向
type Codec interface {
	Name() string
	// Accept 把服务端已接受的连接适配为消息流(必要时完成协议握手)
	Accept(conn net.Conn) (MessageConn, error)
	// Dial 拨号并完成客户端侧握手
	Dial(addr string, timeout time.Duration) (MessageConn, error)
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Codec)
)

// Register 注册协议适配器(重名覆盖,便于测试替换)
func Register(name string, c Codec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = c
}

// Get 按名称取协议适配器
func Get(name string) (Codec, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if name == "" {
		name = "raw"
	}
	c, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("codec: unknown codec %q", name)
	}
	return c, nil
}

// RawCodec 裸 TCP:不做任何包装,消息即读取分块
type RawCodec struct{}

func (RawCodec) Name() string { return "raw" }

func (RawCodec) Accept(conn net.Conn) (MessageConn, error) {
	return &rawConn{conn: conn}, nil
}

func (RawCodec) Dial(addr string, timeout time.Duration) (MessageConn, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &rawConn{conn: conn}, nil
}

type rawConn struct{ conn net.Conn }

func (c *rawConn) ReadMessage() ([]byte, error) {
	buf := make([]byte, 32*1024)
	n, err := c.conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (c *rawConn) WriteMessage(payload []byte) error {
	_, err := c.conn.Write(payload)
	return err
}

func (c *rawConn) Close() error { return c.conn.Close() }

func init() {
	Register("raw", RawCodec{})
}
