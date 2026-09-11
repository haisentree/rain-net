// Package sniff 提供"单端口多协议"的嗅探分发 ingress 装饰器(R7,nps pmux 同思路):
// 一个物理端口上按首部字节区分协议,把连接分发给注册在案的各个目标 net.Listener。
//
// 分发规则(按序):
//  1. 前缀规则:连接首字节匹配某目标的配置前缀(如 star 魔数);
//  2. HTTP:首部字节为 HTTP 方法前缀(GET/POST/…),再读 Host 头——
//     host 命中 hostRoutes 的进对应目标,否则进 httpDefault;
//  3. 其余(TLS 握手 0x16 开头等)→ default 目标。
//
// 嗅探读走的字节会重放(replay)给目标连接,对目标完全透明。
package sniff

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config 嗅探分发配置。
type Config struct {
	// HTTPMethods 视为 HTTP 的方法前缀(默认常见八法)
	HTTPMethods []string
	// HostRoutes HTTP 流量按 Host 头分发:host(小写)→ 目标名
	HostRoutes map[string]string
	// HTTPDefault HTTP 流量未命中 Host 路由时的目标
	HTTPDefault string
	// Prefixes 前缀规则:前缀(hex 编码)→ 目标名,按声明顺序匹配
	Prefixes []PrefixRule
	// Default 兜底目标(TLS 等未识别流量)
	Default string
}

type PrefixRule struct {
	Prefix string // hex 编码
	Target string
}

type target struct {
	name string
	ch   chan net.Conn
	done chan struct{}
}

// Dispatcher 嗅探分发器:实现 net.Listener 的容器。
type Dispatcher struct {
	mu      sync.RWMutex
	targets map[string]*target
	cfg     Config
	base    net.Listener
	done    chan struct{}
	once    sync.Once
}

// New 以 base 为物理监听建立分发器。调用方随后用 d.Listener(name) 获取各目标 listener。
func New(ctx context.Context, base net.Listener, cfg Config) (*Dispatcher, error) {
	if cfg.Default == "" {
		return nil, fmt.Errorf("sniff: default target required")
	}
	if len(cfg.HTTPMethods) == 0 {
		cfg.HTTPMethods = []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE", "PATCH"}
	}
	d := &Dispatcher{targets: make(map[string]*target), cfg: cfg, base: base, done: make(chan struct{})}
	// 预登记配置中出现的目标
	registered := map[string]bool{cfg.Default: true, cfg.HTTPDefault: true}
	for _, h := range cfg.HostRoutes {
		registered[h] = true
	}
	for _, p := range cfg.Prefixes {
		registered[p.Target] = true
	}
	for name := range registered {
		if name == "" {
			continue
		}
		d.targets[name] = &target{name: name, ch: make(chan net.Conn, 16), done: d.done}
	}
	go d.loop(ctx)
	return d, nil
}

// Listener 返回指定目标的虚拟监听器(标准 net.Listener)。
func (d *Dispatcher) Listener(name string) (net.Listener, error) {
	d.mu.RLock()
	t, ok := d.targets[name]
	d.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sniff: unknown target %q", name)
	}
	return &facade{name: name, t: t, addr: d.base.Addr()}, nil
}

func (d *Dispatcher) loop(ctx context.Context) {
	go func() {
		<-ctx.Done()
		d.Close()
	}()
	for {
		conn, err := d.base.Accept()
		if err != nil {
			d.Close()
			return
		}
		go d.dispatch(conn)
	}
}

// recReader 记录嗅探阶段读走的全部字节,handoff 时随连接重放。
type recReader struct {
	r   io.Reader
	buf bytes.Buffer
}

func (rr *recReader) Read(p []byte) (int, error) {
	n, err := rr.r.Read(p)
	rr.buf.Write(p[:n])
	return n, err
}

// dispatch 嗅探并分发一条连接;重放字节 = 本次嗅探读走的全部字节。
func (d *Dispatcher) dispatch(conn net.Conn) {
	rec := &recReader{r: conn}
	head := make([]byte, 3)
	if _, err := io.ReadFull(rec, head); err != nil {
		conn.Close()
		return
	}
	replay := func() []byte {
		out := make([]byte, 0, len(head)+rec.buf.Len())
		out = append(out, head...)
		out = append(out, rec.buf.Bytes()...)
		return out
	}

	// 1) 前缀规则(配置优先)
	for _, p := range d.cfg.Prefixes {
		prefix, err := hexBytes(p.Prefix)
		if err != nil || len(prefix) == 0 {
			continue
		}
		cmp := head
		if len(prefix) > len(cmp) {
			rest := make([]byte, len(prefix)-len(cmp))
			if _, err := io.ReadFull(rec, rest); err != nil {
				conn.Close()
				return
			}
			cmp = append(append([]byte{}, cmp...), rest...)
		}
		if bytes.HasPrefix(cmp, prefix) {
			d.handoff(conn, p.Target, replay())
			return
		}
	}

	// 2) HTTP:方法前缀匹配,再按 Host 细分
	if t, ok := d.matchHTTP(head, rec); ok {
		d.handoff(conn, t, replay())
		return
	}

	// 3) 兜底
	d.handoff(conn, d.cfg.Default, replay())
}

func (d *Dispatcher) matchHTTP(head []byte, rec *recReader) (string, bool) {
	method := string(head)
	hit := false
	for _, m := range d.cfg.HTTPMethods {
		if len(m) >= 3 && strings.HasPrefix(method, m[:3]) {
			hit = true
			break
		}
	}
	if !hit {
		return "", false
	}

	// 读到 Host 头为止(多读的字节已记入 rec,handoff 全量重放)
	br := bufio.NewReader(rec)
	if _, err := br.ReadString('\n'); err != nil {
		return d.cfg.HTTPDefault, true
	}
	hostLine := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(trim), "host:") {
			hostLine = strings.TrimSpace(trim[5:])
			break
		}
	}
	host := strings.ToLower(hostLine)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if t, ok := d.cfg.HostRoutes[host]; ok {
		return t, true
	}
	return d.cfg.HTTPDefault, true
}

// handoff 把连接(连同嗅探读走的全部字节)交给目标队列。
func (d *Dispatcher) handoff(conn net.Conn, targetName string, replay []byte) {
	d.mu.RLock()
	t, ok := d.targets[targetName]
	if !ok {
		t = d.targets[d.cfg.Default]
	}
	d.mu.RUnlock()
	if t == nil {
		conn.Close()
		return
	}
	select {
	case t.ch <- newReplayConn(conn, replay):
	case <-time.After(10 * time.Second):
		conn.Close()
	}
}

func hexBytes(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, " ", "")
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s)+1 && i+2 <= len(s); i += 2 {
		v, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out = append(out, byte(v))
	}
	return out, nil
}

// Close 停止嗅探并关闭物理监听。
func (d *Dispatcher) Close() error {
	d.once.Do(func() { close(d.done) })
	return d.base.Close()
}

// facade 目标虚拟监听器。
type facade struct {
	name string
	t    *target
	addr net.Addr
}

func (f *facade) Accept() (net.Conn, error) {
	select {
	case c := <-f.t.ch:
		return c, nil
	case <-f.t.done:
		return nil, net.ErrClosed
	}
}

func (f *facade) Close() error   { return nil }
func (f *facade) Addr() net.Addr { return f.addr }

// replayConn 把嗅探读走的首部字节在头部重放,后续读透传底层连接。
type replayConn struct {
	net.Conn
	replay []byte
	read   int
}

func newReplayConn(conn net.Conn, replay []byte) net.Conn {
	return &replayConn{Conn: conn, replay: replay}
}

func (r *replayConn) Read(p []byte) (int, error) {
	if r.read < len(r.replay) {
		n := copy(p, r.replay[r.read:])
		r.read += n
		return n, nil
	}
	return r.Conn.Read(p)
}
