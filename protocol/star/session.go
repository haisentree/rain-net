package star

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

// Session 在一条 net.Conn 上按 star 帧格式收发消息
//
// 约定:Receive 阻塞读一条消息,只能由一个 goroutine 串行调用;
// Send 有写锁保护,可以被多个 goroutine 并发调用。
type Session struct {
	conn   net.Conn
	reader *bufio.Reader

	writeMu sync.Mutex
}

func NewSession(conn net.Conn) *Session {
	return &Session{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// Send 发送一条消息,可并发调用
func (s *Session) Send(m *Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return WriteMessage(s.conn, m)
}

// Receive 阻塞读取一条消息,同一时刻只能由一个 goroutine 调用
func (s *Session) Receive() (*Message, error) {
	return ReadMessage(s.reader)
}

// Conn 返回底层连接,用于设置超时等
func (s *Session) Conn() net.Conn { return s.conn }

// Close 关闭底层连接
func (s *Session) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// deadline 辅助:设置一段时间后过期的读写超时
func (s *Session) deadline(d time.Duration) {
	s.conn.SetDeadline(time.Now().Add(d))
}

// clearDeadline 清除超时设置
func (s *Session) clearDeadline() {
	s.conn.SetDeadline(time.Time{})
}

// Ping 同步发送保活探测并等待 Pong,返回往返耗时
//
// 注意:内部会调用 Receive,适用于还没有独立读循环的简单场景;
// 有独立读循环后,应由读循环处理 Ping/Pong,不再使用本方法
func (s *Session) Ping(timeout time.Duration) (time.Duration, error) {
	s.deadline(timeout)
	defer s.clearDeadline()

	start := time.Now()
	if err := s.Send(&Message{Type: MsgPing, StreamID: CtrlStreamID}); err != nil {
		return 0, fmt.Errorf("star: send ping: %w", err)
	}

	msg, err := s.Receive()
	if err != nil {
		return 0, fmt.Errorf("star: read pong: %w", err)
	}
	if msg.Type != MsgPong {
		return 0, fmt.Errorf("star: unexpected message %s while waiting pong", msg.TypeName())
	}
	return time.Since(start), nil
}
