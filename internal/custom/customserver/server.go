package customserver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"rain-net/internal/custom/plugin"
	custom "rain-net/protocol/custom"
)

// custom启动指定端口的
type Server struct {
	Key     string
	Addr    string
	Net     string
	Service string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	zones  *Config
	server *custom.Server
	m      sync.Mutex
}

// 如果Server只支持TCP或者UDP,对应不存在的方法返回nil
func NewServer(serviceName string, host Host, config *Config) (*Server, error) {
	server := &Server{
		Key:     host.Key,
		Addr:    host.Address,
		Net:     host.Network,
		Service: serviceName,

		zones: config,

		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	if server.zones == nil {
		fmt.Println("warning: server zones config is nil")
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
	s.server = &custom.Server{
		Listener:     l,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,

		Handler: custom.HandlerFunc(func(w custom.ResponseWriter, r *custom.Msg) {
			ctx := context.Background()
			fmt.Println("handle:", s.zones.PluginChain.Name())
			s.zones.PluginChain.ServeCustom(ctx, w, r)
		}),
	}

	s.m.Unlock()
	return s.server.ActivateAndServe()
}

func (s *Server) ServePacket(p net.PacketConn) (err error) {
	s.m.Lock()

	s.server = &custom.Server{
		PacketConn:   p,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,

		Handler: custom.HandlerFunc(func(w custom.ResponseWriter, r *custom.Msg) {
			ctx := context.Background()
			fmt.Println("handle:", s.zones.PluginChain.Name())
			s.zones.PluginChain.ServeCustom(ctx, w, r)
		}),
	}
	s.m.Unlock()
	return s.server.ActivateAndServe()
}

func (s *Server) Listen() (net.Listener, error) {
	if s.Net != "tcp" {
		return nil, nil
	}

	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return nil, err
	}
	return l, nil
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

func (s *Server) ServeCustom(ctx context.Context, w custom.ResponseWriter, r *custom.Msg) {
	fmt.Println("s.zones.PluginChain:", s.zones.PluginChain.Name())
	s.zones.PluginChain.ServeCustom(ctx, w, r)
}
