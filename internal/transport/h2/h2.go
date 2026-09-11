// Package h2 提供基于 HTTP/2(h2c 明文)的传输:一条 h2 流 = 一条逻辑连接。
//
// 设计(R5,docs/roadmap.md):
//   - 复用:单个 Transport 实例持有一个共享的 http2.Client,同目标的 Dial
//     自动复用物理连接(http2 协议内建多路复用)——R6 QUIC 的池接口参照此形态;
//   - 半关闭:客户端角色 CloseWrite = 关闭请求体管道(END_STREAM),
//     服务端读到请求体 EOF;服务端角色不支持半关闭(应答结束即流结束),
//     conformance 半关闭场景由拨号(客户端)侧断言;
//   - 伪装:path 可配置(默认 "/"),host 即目标地址,可与反代共存;
//   - 流控:h2 内建连接级/流级窗口,Write 在窗口耗尽时自然阻塞(背压免费);
//   - deadline:读侧经常驻读泵 + 定时器实现(SetReadDeadline 生效于单次 Read)。
package h2

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"

	"github.com/haisentree/rain-net/internal/transport"
)

const (
	defaultPath = "/"
	readChunk   = 16 << 10
	readQueue   = 64 // 读泵队列上限(帧数),背压 ~1MB
)

type h2Transport struct {
	path   string
	client *http.Client
	server *http2.Server
}

func init() {
	transport.Register("h2", func(opts transport.Options) (transport.Transport, error) {
		path := opts.Path
		if path == "" {
			path = defaultPath
		}
		// h2c:AllowHTTP + 明文拨号(加密可由外层 tls transport 叠加)
		tr := &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
		return &h2Transport{
			path:   path,
			client: &http.Client{Transport: tr},
			server: &http2.Server{},
		}, nil
	})
}

// Listen 在真实 TCP 监听上逐连接运行 h2 服务;每个请求 = 一条逻辑连接,
// 经 accept channel 交给 facade listener(形状与 ws transport 一致)。
func (t *h2Transport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	raw, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	ln := &h2Listener{addr: raw.Addr(), conns: make(chan net.Conn), done: make(chan struct{})}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != t.path {
			http.NotFound(w, r)
			return
		}
		sc := newServerStreamConn(w, r)
		select {
		case ln.conns <- sc:
		case <-ln.done:
			sc.Close()
			return
		}
		// 保持在 handler 内直到连接结束,避免响应提前终止
		<-sc.done
	})
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go func() {
		for {
			conn, err := raw.Accept()
			if err != nil {
				ln.Close()
				return
			}
			go t.server.ServeConn(conn, &http2.ServeConnOpts{Handler: handler})
		}
	}()
	return &listenerGuard{
		Listener: ln,
		onClose: func() {
			ln.Close()
			raw.Close()
		},
	}, nil
}

type h2Listener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
}

func (l *h2Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *h2Listener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *h2Listener) Addr() net.Addr { return l.addr }

type listenerGuard struct {
	net.Listener
	onClose func()
	once    sync.Once
}

func (l *listenerGuard) Close() error {
	l.once.Do(l.onClose)
	return l.Listener.Close()
}

// Dial 经共享 http2.Client 发起 POST:响应体即下行,请求体管道即上行。
// 同目标的 Dial 复用同一物理 h2 连接(http2 多路复用内建)。
func (t *h2Transport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, "http://"+addr+t.path, pr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("h2: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	// 建立阶段的 ctx 看门狗:拨号 ctx 取消(超时/调用方取消)则终止请求;
	// Dial 成功后看门狗退出,连接生命周期只由 Close 控制
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-watchDone:
		}
	}()

	resp, err := t.client.Do(req)
	close(watchDone)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("h2: dial %s: %w", addr, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("h2: dial %s: unexpected status %d", addr, resp.StatusCode)
	}

	return newClientStreamConn(resp, cancel, pw), nil
}

var (
	_ net.Conn = (*clientStreamConn)(nil)
	_ net.Conn = (*serverStreamConn)(nil)
)

// errReadTimeout 实现 Timeout() 语义(os.IsTimeout 可识别)
var errReadTimeout error = timeoutError{}

type timeoutError struct{}

func (timeoutError) Error() string   { return "h2: read deadline exceeded" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type streamItem struct {
	data []byte
	err  error
}

// clientStreamConn 拨号侧的 h2 流连接:常驻读泵把响应体搬进队列,
// Read 从队列取(带 deadline);Write 直写请求体管道(h2 流控背压)。
type clientStreamConn struct {
	resp   *http.Response
	cancel context.CancelFunc
	pw     *io.PipeWriter

	queue     chan streamItem
	mu        sync.Mutex
	deadline  time.Time
	pending   []byte
	readEOF   atomic.Bool
	writeOnce sync.Once
	closeOnce sync.Once
	done      chan struct{}
}

func newClientStreamConn(resp *http.Response, cancel context.CancelFunc, pw *io.PipeWriter) *clientStreamConn {
	c := &clientStreamConn{
		resp:   resp,
		cancel: cancel,
		pw:     pw,
		queue:  make(chan streamItem, readQueue),
		done:   make(chan struct{}),
	}
	// 常驻读泵:响应体 → 队列;EOF 以 err 标记入队保序
	go func() {
		buf := make([]byte, readChunk)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case c.queue <- streamItem{data: chunk}:
				case <-c.done:
					return
				}
			}
			if err != nil {
				select {
				case c.queue <- streamItem{err: err}:
				case <-c.done:
				}
				return
			}
		}
	}()
	return c
}

func (c *clientStreamConn) Read(p []byte) (int, error) {
	for {
		if c.readEOF.Load() {
			return 0, io.EOF
		}
		if len(c.pending) > 0 {
			n := copy(p, c.pending)
			c.pending = c.pending[n:]
			return n, nil
		}

		var timeout <-chan time.Time
		c.mu.Lock()
		if !c.deadline.IsZero() {
			d := time.Until(c.deadline)
			c.mu.Unlock()
			if d <= 0 {
				return 0, errReadTimeout
			}
			timer := time.NewTimer(d)
			defer timer.Stop()
			timeout = timer.C
		} else {
			c.mu.Unlock()
		}

		select {
		case item := <-c.queue:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					c.readEOF.Store(true)
					return 0, io.EOF
				}
				return 0, item.err
			}
			if len(item.data) == 0 {
				continue
			}
			n := copy(p, item.data)
			if n < len(item.data) {
				c.pending = item.data[n:]
			}
			return n, nil
		case <-timeout:
			return 0, errReadTimeout
		case <-c.done:
			c.readEOF.Store(true)
			return 0, io.EOF
		}
	}
}

func (c *clientStreamConn) Write(p []byte) (int, error) {
	return c.pw.Write(p)
}

// CloseWrite 半关闭:关闭请求体管道(h2 END_STREAM),服务端读到 EOF;读方向继续。
func (c *clientStreamConn) CloseWrite() error {
	c.writeOnce.Do(func() { c.pw.Close() })
	return nil
}

// Close 取消请求上下文:流终止。
func (c *clientStreamConn) Close() error {
	c.writeOnce.Do(func() { c.pw.Close() })
	c.closeOnce.Do(func() { c.cancel() })
	return nil
}

func (c *clientStreamConn) LocalAddr() net.Addr  { return addr{network: "h2"} }
func (c *clientStreamConn) RemoteAddr() net.Addr { return addr{network: "h2"} }

func (c *clientStreamConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *clientStreamConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *clientStreamConn) SetWriteDeadline(time.Time) error {
	return nil // 写侧经 h2 流控背压,deadline v1 不生效
}

// serverStreamConn 服务端侧:h2 请求体即上行,ResponseWriter 即下行。
// 不支持半关闭:应答结束(handler 返回)即流结束。
type serverStreamConn struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	r         io.Reader
	done      chan struct{}
	closeOnce sync.Once
}

func newServerStreamConn(w http.ResponseWriter, r *http.Request) *serverStreamConn {
	f, _ := w.(http.Flusher)
	// 立即写出响应头并 Flush:拨号侧 client.Do 等的就是这组头(连接就此建立)
	w.WriteHeader(http.StatusOK)
	if f != nil {
		f.Flush()
	}
	// done 只由使用方的 Close 关闭:连接断开时 r.Body.Read 自然报错,
	// 消费方收尾后 handler 才返回——保证不会有 Write 落在 handler 返回之后
	return &serverStreamConn{w: w, flusher: f, r: r.Body, done: make(chan struct{})}
}

func (s *serverStreamConn) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return n, err
}

// Write 每次写入后 Flush,保证数据即时到达对端。
func (s *serverStreamConn) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return n, err
}

func (s *serverStreamConn) Close() error {
	// 服务端无法单独结束响应写侧:结束流 = handler 返回(整条终止)
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

func (s *serverStreamConn) LocalAddr() net.Addr  { return addr{network: "h2"} }
func (s *serverStreamConn) RemoteAddr() net.Addr { return addr{network: "h2"} }
func (s *serverStreamConn) SetDeadline(time.Time) error {
	return nil
}
func (s *serverStreamConn) SetReadDeadline(time.Time) error  { return nil }
func (s *serverStreamConn) SetWriteDeadline(time.Time) error { return nil }

type addr struct{ network string }

func (a addr) Network() string { return a.network }
func (a addr) String() string  { return "h2-stream" }
