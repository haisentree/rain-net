// Package kcp 提供基于 KCP(ARQ)+ smux 多路复用的传输:
// 一条 smux 流 = 一条逻辑连接,多条流复用一条 KCP/UDP 通道。
//
// 设计(R9 加餐,docs/roadmap.md):
//   - 连接池:Dial 按 (地址) 复用池中的 smux 会话,每次开新流——与 quic/h2 的池同构;
//   - 半关闭:smux 流仅有整条 Close,不支持半关闭 → conformance 走降级场景
//     (docs/design.md 半关闭矩阵);
//   - 分层组合:KCP(ARQ/FEC/拥塞控制)→ smux(多路复用)→ 业务流,
//     与 kcptun 的 "smux over kcp" 形态一致。
package kcp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	kcp "github.com/xtaci/kcp-go"
	"github.com/xtaci/smux"

	"github.com/haisentree/rain-net/internal/transport"
)

type kcpTransport struct {
	mu       sync.Mutex
	sessions map[string]*smux.Session // 客户端会话池:地址 → smux 会话

	serverConf *smux.Config
	clientConf *smux.Config
}

func init() {
	transport.Register("kcp", func(transport.Options) (transport.Transport, error) {
		serverConf := smux.DefaultConfig()
		clientConf := smux.DefaultConfig()
		return &kcpTransport{
			sessions:   make(map[string]*smux.Session),
			serverConf: serverConf,
			clientConf: clientConf,
		}, nil
	})
}

// Listen 在 UDP 地址上接受 KCP 连接;每条连接升级为 smux 会话,
// 会话上的每条流 = 一条逻辑连接(经 facade listener 输出标准 net.Conn)。
func (t *kcpTransport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	ln, err := kcp.Listen(addr)
	if err != nil {
		return nil, fmt.Errorf("kcp: listen %s: %w", addr, err)
	}
	kl := &kcpListener{ln: ln, conns: make(chan net.Conn, 16), done: make(chan struct{})}
	go func() {
		<-ctx.Done()
		kl.Close()
	}()
	go kl.acceptLoop(t.serverConf)
	return kl, nil
}

type kcpListener struct {
	ln    net.Listener
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *kcpListener) acceptLoop(conf *smux.Config) {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			l.Close()
			return
		}
		go func() {
			sess, err := smux.Server(conn, conf)
			if err != nil {
				conn.Close()
				return
			}
			for {
				stream, err := sess.AcceptStream()
				if err != nil {
					return
				}
				select {
				case l.conns <- stream:
				case <-l.done:
					stream.Close()
					return
				}
			}
		}()
	}
}

func (l *kcpListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *kcpListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.ln.Close()
}

func (l *kcpListener) Addr() net.Addr { return l.ln.Addr() }

// Dial 取池中的 smux 会话(死会话自动重建),开新流。
func (t *kcpTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := t.getPooled(ctx, addr)
	if err != nil {
		return nil, err
	}
	stream, err := sess.OpenStream()
	if err != nil {
		t.drop(addr, sess)
		return nil, fmt.Errorf("kcp: open stream to %s: %w", addr, err)
	}
	return stream, nil
}

func (t *kcpTransport) getPooled(ctx context.Context, addr string) (*smux.Session, error) {
	t.mu.Lock()
	if s, ok := t.sessions[addr]; ok {
		t.mu.Unlock()
		if !s.IsClosed() {
			return s, nil
		}
		t.drop(addr, s)
	}
	t.mu.Unlock()

	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := dialKCP(dctx, addr)
	if err != nil {
		return nil, fmt.Errorf("kcp: dial %s: %w", addr, err)
	}
	sess, err := smux.Client(conn, t.clientConf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("kcp: smux client: %w", err)
	}

	t.mu.Lock()
	// 并发拨号去重:优先复用已有会话
	if exist, ok := t.sessions[addr]; ok && !exist.IsClosed() {
		t.mu.Unlock()
		sess.Close()
		return exist, nil
	}
	t.sessions[addr] = sess
	t.mu.Unlock()
	return sess, nil
}

func (t *kcpTransport) drop(addr string, sess *smux.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if exist, ok := t.sessions[addr]; ok && exist == sess {
		delete(t.sessions, addr)
	}
	sess.Close()
}

func dialKCP(ctx context.Context, addr string) (net.Conn, error) {
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		conn, err := kcp.Dial(addr)
		ch <- res{conn, err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
