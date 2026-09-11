package starserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/bridge"
	"github.com/haisentree/rain-net/internal/client"
	"github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/transport"
	"github.com/haisentree/rain-net/pkg/rainnet"
	"github.com/haisentree/rain-net/pluginer"
	"github.com/haisentree/rain-net/protocol/codec"
	"github.com/haisentree/rain-net/protocol/star"
)

// ProxyServer 数据面代理监听器(type: proxy)
//
// 外部用户的每条连接经 connect 规则路由到一条已注册的服务流:
// round-robin 挑选目标 -> hub.OpenConn 请求客户端拨通内网服务 ->
// 双向搬运:外部连接 <-> ctrl 通道数据帧(帧 streamID 字段为连接ID)
type ProxyServer struct {
	Name string
	Net  string
	Addr string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// proxyName 逻辑代理名,匹配 ConnectTable 里的规则
	proxyName string
	// connects 共享的路由规则表,运行时可被 admin API 动态增删
	connects *ConnectTable
	// inCodec 入站协议适配器(raw/ws)
	inCodec codec.Codec

	settings Settings
	hub      *starhandler.CtrlHub
	rr       atomic.Uint32

	// 出站链路(场景 C):本节点内嵌客户端,把连接经隧道推给远端节点落地
	chainRoutes  []ChainRoute
	chainSpecs   map[string]config.DialerSpec
	chainDialers map[string]*client.Dialer
	chainMu      sync.Mutex
	chainRR      atomic.Uint32

	injected net.Listener // 外部注入的监听器(嗅探分发),非 nil 时优先
}

// UseListener 注入外部监听器(嗅探分发等装配场景),优先于按传输自建
func (s *ProxyServer) UseListener(ln net.Listener) *ProxyServer {
	s.injected = ln
	return s
}

// ProxyOption 代理监听器可选配置。
type ProxyOption func(*ProxyServer)

// WithChainRoutes 配置出站链路(design.md §六 场景 C:节点串联的网关侧)。
func WithChainRoutes(routes []ChainRoute, specs map[string]config.DialerSpec) ProxyOption {
	return func(p *ProxyServer) {
		p.chainRoutes = routes
		p.chainSpecs = specs
	}
}

var _ pluginer.Server = (*ProxyServer)(nil)

func NewProxyServer(serviceName string, listener *ListenerList, connects *ConnectTable, hub *starhandler.CtrlHub, opts ...ProxyOption) (*ProxyServer, error) {
	// 入站协议适配器:raw(默认)/ws,协议转化在这一层发生
	c, err := codec.Get(listener.Settings.InCodec)
	if err != nil {
		return nil, err
	}

	p := &ProxyServer{
		Name: serviceName,
		Net:  listener.Transport,
		Addr: listener.Addr,

		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,

		proxyName: listener.Settings.ProxyName,
		connects:  connects,
		settings:  listener.Settings,
		hub:       hub,
		inCodec:   c,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// pickEntry round-robin 挑选一个当前可用的服务流
func (s *ProxyServer) pickEntry() *starhandler.StreamEntry {
	targets := s.connects.For(s.proxyName)
	n := len(targets)
	if n == 0 {
		return nil
	}
	start := int(s.rr.Add(1))

	for i := 0; i < n; i++ {
		t := targets[(start+i)%n]
		if t.StreamId != "" {
			if e, ok := s.hub.Streams.GetByName(t.ClientProxyName, t.StreamId); ok {
				return e
			}
			continue
		}
		// 未指定 streamId 时取该客户端代理名下的第一条流
		for _, e := range s.hub.Streams.FindByClientProxy(t.ClientProxyName) {
			return e
		}
	}
	return nil
}

// Listen 经传输注册表构建(tcp/tls/ws/h2/quic 皆可)——传输转换矩阵的入站半边;
// 注入监听器(单端口嗅探场景)优先
func (s *ProxyServer) Listen() (net.Listener, error) {
	if s.injected != nil {
		return s.injected, nil
	}
	if s.Net == "udp" {
		return nil, nil // 数据面 v1 只做流式传输
	}
	tr, err := transport.Build(s.Net, transport.Options{
		CertFile: s.settings.CertFile,
		KeyFile:  s.settings.KeyFile,
	})
	if err != nil {
		return nil, err
	}
	return tr.Listen(context.Background(), s.Addr)
}

func (s *ProxyServer) ListenPacket() (net.PacketConn, error) {
	// 数据面当前只走 TCP
	return nil, nil
}

func (s *ProxyServer) Serve(l net.Listener) error {
	// 出站链路:内嵌客户端拨向远端节点并注册流(场景 C 网关侧)
	s.chainMu.Lock()
	for name, spec := range s.chainSpecs {
		if _, running := s.chainDialers[name]; running {
			continue
		}
		var streams []star.RegisterStreamReq
		for _, cr := range s.chainRoutes {
			if cr.Dialer != name {
				continue
			}
			streams = append(streams, star.RegisterStreamReq{
				ClientProxyName: cr.Identity, StreamId: cr.StreamId,
			})
		}
		d := client.NewDialer(name, spec.Addr, spec.KeyPassword, streams)
		d.TLS = spec.Transport == "tls"
		d.TLSSkipVerify = true // 节点间自签名场景;生产可配 CA
		d.LocalDialer = func(string) (net.Conn, error) {
			return nil, errors.New("chain dialer does not serve local streams")
		}
		if s.chainDialers == nil {
			s.chainDialers = make(map[string]*client.Dialer)
		}
		s.chainDialers[name] = d
		go func() { _ = d.Run() }()
	}
	s.chainMu.Unlock()

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *ProxyServer) ServePacket(p net.PacketConn) error {
	// 数据面当前只走 TCP
	return nil
}

func (s *ProxyServer) handleConn(conn net.Conn) {
	// 出站链路:连接经内嵌客户端的隧道推给远端节点落地(裸字节,见场景 C)
	if len(s.chainRoutes) > 0 {
		s.serveChain(conn)
		return
	}

	// 入站协议适配:raw 直接包装,ws 等在此完成协议握手
	mc, err := s.inCodec.Accept(conn)
	if err != nil {
		slog.Warn("proxy codec accept", "listener", s.Name, "err", err)
		conn.Close()
		return
	}

	entry := s.pickEntry()
	if entry == nil {
		slog.Warn("proxy no available stream", "listener", s.Name, "remote", conn.RemoteAddr())
		mc.Close()
		return
	}

	e, err := s.hub.OpenConn(entry)
	if err != nil {
		slog.Warn("proxy open conn", "listener", s.Name, "err", err)
		mc.Close()
		return
	}
	slog.Info("proxy conn via stream", "conn", e.ConnID, "stream", e.StreamId,
		"codec", s.inCodec.Name(), "remote", conn.RemoteAddr())

	// 生命周期与 R2 Bridge 状态机同构:两个方向独立收尾,
	// 双方向都结束(或任一方向出错)才整条拆除。
	// 上行 EOF → 半关帧通知客户端;下行半关帧 → 排空后对外部 CloseWrite(支持则)。
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	var (
		teardownOnce sync.Once
		upDone       atomic.Bool
		downDone     atomic.Bool
	)
	teardown := func() {
		teardownOnce.Do(func() {
			stop()
			s.hub.CloseConn(e.ConnID, true)
			mc.Close()
		})
	}
	defer teardown()
	bothFinished := func() {
		if upDone.Load() && downDone.Load() {
			teardown()
		}
	}

	// 下行泵:ctrl 通道 -> 外部连接(按入站协议编码)
	go func() {
		defer func() { stop() }()
		for {
			select {
			case payload := <-e.Sink:
				println("PUMP write:", string(payload))
				if err := mc.WriteMessage(payload); err != nil {
					teardown()
					return
				}
				e.ReleaseRecv(len(payload)) // 归还客户端发送额度(流控)
			case <-e.RemoteClosed:
				// 客户端内网侧写完:排空在途数据。
				// 读循环与 markRemoteClosed 并发,静默窗口兜住在途帧(50ms 无新数据才收尾)
				quiet := false
				for {
					select {
					case payload := <-e.Sink:
						if err := mc.WriteMessage(payload); err != nil {
							teardown()
							return
						}
						e.ReleaseRecv(len(payload))
						quiet = false
					default:
						if quiet {
							downDone.Store(true)
							// 支持半关闭则只关外部写侧,外部仍可继续上行;否则降级整条关闭
							if cw, ok := mc.(interface{ CloseWrite() error }); ok && cw.CloseWrite() == nil {
								bothFinished()
								return
							}
							teardown()
							return
						}
						quiet = true
						time.Sleep(20 * time.Millisecond)
					}
				}
			case <-e.Done:
				teardown()
				return
			case <-done:
				return
			}
		}
	}()

	// 上行:外部连接 -> ctrl 通道(按入站协议解码,ws 消息保留边界)
	upCtx, upCancel := context.WithCancel(context.Background())
	defer upCancel()
	for {
		payload, err := mc.ReadMessage()
		if err != nil {
			slog.Warn("DBG uplink read ended", "err", err, "isEOF", errors.Is(err, io.EOF))
			if errors.Is(err, io.EOF) {
				// 外部用户不再上行:半关帧通知客户端(内网连接关写),
				// 本方向结束,等下行泵收尾
				upDone.Store(true)
				_ = e.Client.Send(star.HalfClose(e.ConnID))
				bothFinished()
				<-done
				return
			}
			return // defer teardown 整条拆除
		}
		// 流控:外部→客户端方向受发送窗口约束(客户端消费后归还额度)
		if err := e.AcquireSend(upCtx, len(payload)); err != nil {
			return
		}
		if err := e.Client.Send(&star.Message{
			Type:     star.MsgData,
			StreamID: e.ConnID,
			Payload:  payload,
		}); err != nil {
			return
		}
		if e.Stream != nil {
			e.Stream.InBytes.Add(uint64(len(payload)))
		}
	}
}

// pickChain round-robin 挑选出站链路路由,返回对应已启动的内嵌客户端。
func (s *ProxyServer) pickChain() (ChainRoute, *client.Dialer) {
	n := len(s.chainRoutes)
	if n == 0 {
		return ChainRoute{}, nil
	}
	cr := s.chainRoutes[s.chainRR.Add(1)%uint32(n)]
	s.chainMu.Lock()
	d := s.chainDialers[cr.Dialer]
	s.chainMu.Unlock()
	return cr, d
}

// serveChain 出站链路:用户连接 ↔ 隧道链路连接(远端落地),半关闭经 Bridge 传播。
func (s *ProxyServer) serveChain(conn net.Conn) {
	cr, d := s.pickChain()
	if d == nil {
		conn.Close()
		return
	}

	// 等内嵌客户端会话建立(首次启动时拨号中)
	deadline := time.Now().Add(10 * time.Second)
	var cc *star.CtrlClient
	for time.Now().Before(deadline) {
		if cc = d.Client(); cc != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cc == nil {
		slog.Warn("chain gateway: client session not ready", "dialer", cr.Dialer)
		conn.Close()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tconn, err := cc.DialIntranet(ctx, cr.Identity, cr.StreamId)
	if err != nil {
		slog.Warn("chain gateway dial intranet", "stream", cr.StreamId, "err", err)
		conn.Close()
		return
	}
	defer conn.Close()
	defer tconn.Close()

	slog.Info("chain gateway serving", "ingress", s.Name, "dialer", cr.Dialer, "stream", cr.StreamId,
		"remote", conn.RemoteAddr())
	if err := bridge.Bridge(context.Background(),
		rainnet.Wrap(conn, rainnet.Meta{Listener: s.Name}),
		rainnet.Wrap(tconn, rainnet.Meta{Identity: cr.Identity})); err != nil {
		slog.Debug("chain bridge ended", "err", err)
	}
}
