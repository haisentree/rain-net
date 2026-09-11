// Package ws 提供基于 WebSocket 的传输:任意字节流承载于 binary 帧。
//
// 帧到流的映射(R4 决策,docs/roadmap.md):
//   - 写侧:每次 Write 作为一个 binary 帧发出(io.Copy 的 64KB 块自然成帧);
//   - 读侧:逐帧读取,帧载荷按序拼接为字节流(消息边界不保留);
//   - ws 无半关闭语义:CloseWrite 不受支持,半关闭在转换链上降级为整条关闭
//     (docs/design.md 半关闭矩阵 ws 列);
//   - ping 帧自动回 pong;close 帧视作 EOF;单帧上限 1MB(防恶意声明)。
package ws

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"

	"github.com/haisentree/rain-net/internal/transport"
)

// maxFrameSize 单帧载荷上限
const maxFrameSize = 1 << 20

type wsTransport struct {
	path string // 握手路径,默认 "/"
}

func init() {
	transport.Register("ws", func(opts transport.Options) (transport.Transport, error) {
		return &wsTransport{path: opts.Path}, nil
	})
}

func (t *wsTransport) handshakePath() string {
	if t.path == "" {
		return "/"
	}
	return t.path
}

// Listen 启动 HTTP 服务,在握手路径上升级 WebSocket 连接。
// ctx 取消时关闭 HTTP server 与监听(经 listenerGuard 落实 Listen 契约)。
func (t *wsTransport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	raw, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	ln := &wsListener{addr: raw.Addr(), conns: make(chan net.Conn), done: make(chan struct{})}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != t.handshakePath() {
				http.NotFound(w, r)
				return
			}
			conn, rw, _, err := ws.UpgradeHTTP(r, w)
			if err != nil {
				return
			}
			select {
			case ln.conns <- newServerConn(conn, rw):
			case <-ln.done:
				conn.Close()
			}
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go func() {
		_ = srv.Serve(raw)
	}()
	return &listenerGuard{
		Listener: ln,
		onClose: func() {
			_ = srv.Close()
			_ = raw.Close()
		},
	}, nil
}

type listenerGuard struct {
	net.Listener
	onClose func()
	once    sync.Once
}

func (l *listenerGuard) Close() error {
	l.once.Do(l.onClose)
	return l.Listener.Close()
}

type wsListener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
}

func (l *wsListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *wsListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *wsListener) Addr() net.Addr { return l.addr }

// Dial 建立 ws 客户端连接(完成升级握手)。
func (t *wsTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	url := "ws://" + addr + t.handshakePath()
	conn, br, _, err := ws.Dial(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", addr, err)
	}
	return newClientConn(conn, br), nil
}

type wsAddr struct{ addr string }

func (a wsAddr) Network() string { return "ws" }
func (a wsAddr) String() string  { return a.addr }

// wsConn 把 ws binary 帧流适配为 net.Conn。
// server 角色读到的客户端帧带掩码需解掩码;client 角色写出帧需加掩码。
type wsConn struct {
	raw net.Conn
	br  *bufio.Reader // 握手段可能缓冲的已读数据

	client bool

	readMu  sync.Mutex
	writeMu sync.Mutex
	pending []byte // 当前帧未读尽的载荷
}

var (
	_ net.Conn = (*wsConn)(nil)
	_ net.Conn = (*serverConn)(nil)
)

func newServerConn(raw net.Conn, rw *bufio.ReadWriter) net.Conn {
	var br *bufio.Reader
	if rw != nil {
		br = rw.Reader
	}
	return &wsConn{raw: raw, br: br}
}

func newClientConn(raw net.Conn, br *bufio.Reader) net.Conn {
	// gobwas ws.Dial 在无缓冲余数据时返回 nil reader,统一兜底
	if br == nil {
		br = bufio.NewReader(raw)
	}
	return &wsConn{raw: raw, br: br, client: true}
}

// serverConn 服务端角色的 ws 连接(server 帧不掩码)
type serverConn = wsConn

func (c *wsConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		if len(c.pending) > 0 {
			n := copy(p, c.pending)
			c.pending = c.pending[n:]
			return n, nil
		}

		var (
			h   ws.Header
			r   io.Reader
			err error
		)
		if c.br != nil && c.br.Buffered() > 0 {
			h, err = ws.ReadHeader(c.br)
			r = c.br
		} else {
			h, err = ws.ReadHeader(c.raw)
			r = c.raw
		}
		if err != nil {
			return 0, err
		}
		if err := c.handleFrame(h, r); err != nil {
			return 0, err
		}
	}
}

// handleFrame 读一帧载荷并处理控制帧;data 帧存入 pending。
func (c *wsConn) handleFrame(h ws.Header, r io.Reader) error {
	if h.Length > maxFrameSize {
		return fmt.Errorf("ws: frame size %d exceeds limit %d", h.Length, maxFrameSize)
	}
	payload := make([]byte, h.Length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return err
	}
	if h.Masked {
		ws.Cipher(payload, h.Mask, 0)
	}

	switch h.OpCode {
	case ws.OpBinary, ws.OpText, ws.OpContinuation:
		c.pending = payload
		return nil
	case ws.OpPing:
		return c.writeControl(ws.OpPong, payload)
	case ws.OpPong:
		return nil
	case ws.OpClose:
		_ = c.writeControl(ws.OpClose, payload)
		return io.EOF
	default:
		return fmt.Errorf("ws: unexpected opcode %d", h.OpCode)
	}
}

func (c *wsConn) writeControl(op ws.OpCode, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.client {
		return wsutil.WriteClientMessage(c.raw, op, payload)
	}
	return wsutil.WriteServerMessage(c.raw, op, payload)
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	total := len(p)
	for off := 0; off < total; off += maxFrameSize {
		end := off + maxFrameSize
		if end > total {
			end = total
		}
		var err error
		if c.client {
			err = wsutil.WriteClientMessage(c.raw, ws.OpBinary, p[off:end])
		} else {
			err = wsutil.WriteServerMessage(c.raw, ws.OpBinary, p[off:end])
		}
		if err != nil {
			return off, err
		}
	}
	return total, nil
}

func (c *wsConn) Close() error {
	c.writeMu.Lock()
	if c.client {
		_ = wsutil.WriteClientMessage(c.raw, ws.OpClose, nil)
	} else {
		_ = wsutil.WriteServerMessage(c.raw, ws.OpClose, nil)
	}
	c.writeMu.Unlock()
	return c.raw.Close()
}

func (c *wsConn) LocalAddr() net.Addr               { return c.raw.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr              { return c.raw.RemoteAddr() }
func (c *wsConn) SetDeadline(t time.Time) error     { return c.raw.SetDeadline(t) }
func (c *wsConn) SetReadDeadline(t time.Time) error { return c.raw.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error {
	return c.raw.SetWriteDeadline(t)
}
