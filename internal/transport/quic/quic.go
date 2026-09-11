// Package quic 提供基于 QUIC(RFC 9000)的传输:一条 QUIC 流 = 一条逻辑连接。
//
// 设计(R6,docs/roadmap.md):
//   - 多路复用 + 连接池:Dial 按 (网络, 地址) 复用池中的 quic.Connection,
//     每次开新流——与 h2 的共享 http2.Client 同构,池接口形态保持一致;
//   - 半关闭原生支持:CloseWrite = quic Stream.Close()(发送 FIN,读方向继续),
//     Close = Close + CancelRead(整条终止)——半关闭矩阵全对齐;
//   - 流控:QUIC 内建连接级/流级窗口(背压免费);
//   - 加密:TLS1.3 内嵌握手,证书配置复用 transport.Options(自签名场景
//     tls.InsecureSkipVerify 由 Dial 侧 Options 承载)。
package quic

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/haisentree/rain-net/internal/transport"
)

type quicTransport struct {
	mu         sync.Mutex
	tlsConf    *tls.Config // 出站客户端配置
	conns      map[string]*poolEntry
	client     *quic.Transport // 共享 UDP socket:同目标的连接经其复用
	clientOnce sync.Once

	serverTLS *tls.Config // 入站服务端证书
}

type poolEntry struct {
	conn      *quic.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func (e *poolEntry) close() {
	e.closeOnce.Do(func() {
		close(e.done)
		e.conn.CloseWithError(0, "transport closed")
	})
}

func init() {
	transport.Register("quic", build)
}

func build(opts transport.Options) (transport.Transport, error) {
	t := &quicTransport{conns: make(map[string]*poolEntry)}
	if opts.CertFile != "" || opts.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("quic: load key pair: %w", err)
		}
		t.serverTLS = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
			NextProtos:   []string{"rain-net"},
		}
	}
	t.tlsConf = &tls.Config{
		InsecureSkipVerify: opts.TLSSkipVerify, // 自签名场景;生产应提供受信 CA
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"rain-net"},
	}
	return t, nil
}

// Listen 在 UDP 地址上接受 QUIC 连接;每条连接的每条新流 = 一条逻辑连接。
func (t *quicTransport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	if t.serverTLS == nil {
		return nil, fmt.Errorf("quic: listen requires certFile/keyFile")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("quic: resolve %s: %w", addr, err)
	}
	pc, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	qcfg := &quic.Config{
		MaxStreamReceiveWindow:     4 << 20,  // 4MB:代理场景放大窗口,未读保护阈值随之提高
		MaxConnectionReceiveWindow: 16 << 20, // 16MB
	}
	ln, err := quic.Listen(pc, t.serverTLS, qcfg)
	if err != nil {
		pc.Close()
		return nil, err
	}
	ql := &quicListener{ln: ln, conns: make(chan net.Conn), done: make(chan struct{})}
	go func() {
		<-ctx.Done()
		ql.Close()
	}()
	go ql.acceptLoop()
	return ql, nil
}

type quicListener struct {
	ln    *quic.Listener
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *quicListener) acceptLoop() {
	for {
		conn, err := l.ln.Accept(context.Background())
		if err != nil {
			l.Close()
			return
		}
		go l.streamsOf(conn)
	}
}

// streamsOf 接受一条 QUIC 连接上的全部入站流,逐条送入逻辑连接队列。
func (l *quicListener) streamsOf(conn *quic.Conn) {
	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		select {
		case l.conns <- newStreamConn(conn, stream):
		case <-l.done:
			stream.Close()
			return
		}
	}
}

func (l *quicListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *quicListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.ln.Close()
	})
	return nil
}

func (l *quicListener) Addr() net.Addr { return l.ln.Addr() }

// Dial 取池中到 addr 的 QUIC 连接(无则新建),开新流并适配为 net.Conn。
func (t *quicTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := t.getPooled(ctx, addr)
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.drop(addr, conn)
		return nil, fmt.Errorf("quic: open stream to %s: %w", addr, err)
	}
	return newStreamConn(conn, stream), nil
}

// clientTransport 惰性创建共享的 quic.Transport(单一本地 UDP socket)。
func (t *quicTransport) clientTransport() *quic.Transport {
	t.clientOnce.Do(func() {
		pc, err := net.ListenUDP("udp", nil)
		if err != nil {
			return
		}
		t.client = &quic.Transport{Conn: pc}
	})
	return t.client
}

func (t *quicTransport) getPooled(ctx context.Context, addr string) (*quic.Conn, error) {
	t.mu.Lock()
	if e, ok := t.conns[addr]; ok {
		t.mu.Unlock()
		select {
		case <-e.done:
			// 池中连接已死,重建
		default:
			return e.conn, nil
		}
	}
	t.mu.Unlock()

	tlsConf := t.tlsConf.Clone()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	tlsConf.ServerName = host

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("quic: resolve %s: %w", addr, err)
	}
	tr := t.clientTransport()
	if tr == nil {
		return nil, fmt.Errorf("quic: create client transport: %w", net.ErrClosed)
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	qcfg := &quic.Config{
		MaxStreamReceiveWindow:     4 << 20,
		MaxConnectionReceiveWindow: 16 << 20,
	}
	conn, err := tr.Dial(dctx, udpAddr, tlsConf, qcfg)
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}

	entry := &poolEntry{conn: conn, done: make(chan struct{})}

	t.mu.Lock()
	// 并发拨号时可能已有人建池,优先用已有条目
	if exist, ok := t.conns[addr]; ok {
		t.mu.Unlock()
		select {
		case <-exist.done:
			t.mu.Lock()
			t.conns[addr] = entry
			t.mu.Unlock()
		default:
			// 冗余新建的连接关掉,复用池中已有连接
			entry.close()
			return exist.conn, nil
		}
	} else {
		t.conns[addr] = entry
		t.mu.Unlock()
	}
	return conn, nil
}

func (t *quicTransport) drop(addr string, conn *quic.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.conns[addr]; ok && e.conn == conn {
		delete(t.conns, addr)
	}
}

// streamConn 一条 QUIC 流适配为 net.Conn。
type streamConn struct {
	conn   *quic.Conn
	stream *quic.Stream

	closeOnce sync.Once
	writeOnce sync.Once
}

var _ net.Conn = (*streamConn)(nil)

func newStreamConn(conn *quic.Conn, stream *quic.Stream) *streamConn {
	return &streamConn{conn: conn, stream: stream}
}

func (c *streamConn) Read(p []byte) (int, error)  { return c.stream.Read(p) }
func (c *streamConn) Write(p []byte) (int, error) { return c.stream.Write(p) }

// CloseWrite 半关闭:发送 FIN(对端读到 EOF),本侧读方向继续。
func (c *streamConn) CloseWrite() error {
	c.writeOnce.Do(func() { _ = c.stream.Close() })
	return nil
}

// Close 整条终止:FIN + 取消读侧。
func (c *streamConn) Close() error {
	c.writeOnce.Do(func() { _ = c.stream.Close() })
	c.closeOnce.Do(func() { c.stream.CancelRead(0) })
	return nil
}

func (c *streamConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *streamConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (c *streamConn) SetDeadline(t time.Time) error { return c.stream.SetDeadline(t) }
func (c *streamConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}
func (c *streamConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}
