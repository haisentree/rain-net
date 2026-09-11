package starserver

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/haisentree/rain-net/internal/star/plugin"
	"github.com/haisentree/rain-net/internal/transport"
	"github.com/haisentree/rain-net/protocol/star"
)

type Server struct {
	Name string
	Net  string
	Addr string
	// Type string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	server *star.Server

	zones *Config
	m     sync.Mutex

	transportOpts transport.Options
}

func NewServer(serviceName, transportName, addr string, config *Config, tOpts transport.Options) (*Server, error) {
	server := &Server{
		Name: serviceName,
		Net:  transportName,
		Addr: addr,

		zones: config,

		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,

		transportOpts: tOpts,
	}

	if server.zones == nil {
		slog.Warn("server zones config is nil")
		return nil, nil
	}

	var stack plugin.Handler
	for i := len(server.zones.Plugin) - 1; i >= 0; i-- {
		stack = server.zones.Plugin[i](stack)
	}
	server.zones.PluginChain = stack
	return server, nil
}

func (s *Server) Serve(l net.Listener) (err error) {
	s.m.Lock()
	s.server = &star.Server{
		Net:          s.Net,
		Listener:     l,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,

		Handler: star.HandlerFunc(func(w star.ResponseWriter, data []byte) {
			ctx := context.Background()
			slog.Debug("handle plugin chain", "chain", s.zones.PluginChain.Name())
			s.zones.PluginChain.ServeStar(ctx, w, data)
		}),
	}
	s.m.Unlock()
	return s.server.ActivateAndServe()
}

func (s *Server) ServePacket(p net.PacketConn) (err error) {
	s.m.Lock()

	s.server = &star.Server{
		PacketConn:   p,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,

		Handler: star.HandlerFunc(func(w star.ResponseWriter, data []byte) {
			ctx := context.Background()
			slog.Debug("handle plugin chain", "chain", s.zones.PluginChain.Name())
			s.zones.PluginChain.ServeStar(ctx, w, data)
		}),
	}
	s.m.Unlock()
	return s.server.ActivateAndServe()
}

func (s *Server) Listen() (net.Listener, error) {
	// udp 走 PacketConn 特例(ListenPacket):transport 层 v1 不建模 UDP 数据报
	if s.Net == "udp" {
		return nil, nil
	}
	// 其余传输(tcp/tls/…)一律经 transport 注册表构建,装配层不再硬编码
	tr, err := transport.Build(s.Net, s.transportOpts)
	if err != nil {
		return nil, err
	}
	return tr.Listen(context.Background(), s.Addr)
}

func (s *Server) ListenPacket() (net.PacketConn, error) {
	if s.Net != "udp" {
		return nil, nil
	}

	p, err := net.ListenPacket("udp", s.Addr)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (s *Server) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) {
	slog.Debug("serve plugin chain", "chain", s.zones.PluginChain.Name())
	s.zones.PluginChain.ServeStar(ctx, w, data)
}
