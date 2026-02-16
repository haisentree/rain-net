package starserver

import (
	"context"
	"fmt"
	"net"
	"rain-net/internal/star/plugin"
	"rain-net/pluginer"
	"rain-net/protocol/star"
	"sync"
	"time"
)

type Server struct {
	Name string
	Net  string
	Addr string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	server *star.Server

	zones *Config
	m     sync.Mutex
}

func NewServer(serviceName string, host pluginer.Host, config *Config) (*Server, error) {
	server := &Server{
		Name: serviceName,

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
	s.server = &star.Server{
		Listener:     l,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,

		Handler: star.HandlerFunc(func(w star.ResponseWriter, data []byte) {
			ctx := context.Background()
			fmt.Println("handle:", s.zones.PluginChain.Name())
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
			fmt.Println("handle:", s.zones.PluginChain.Name())
			s.zones.PluginChain.ServeStar(ctx, w, data)
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

func (s *Server) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) {
	fmt.Println("s.zones.PluginChain:", s.zones.PluginChain.Name())
	s.zones.PluginChain.ServeStar(ctx, w, data)
}
