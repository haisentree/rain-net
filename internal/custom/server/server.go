package customserver

import (
	"net"
	"sync"
	"time"

	customP "rain-net/protocol/custom"
)

const (
	tcp = 0
	udp = 1
)

type Server struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	server [2]*customP.Server // 0 is a net.Listener, 1 is a net.PacketConn (a *UDPConn) in our case.
	m      sync.Mutex         // protects the servers
}

func NewServer(addr string) (*Server, error) {
	if len(addr) == 0 {
		addr = "127.0.0.1:20000"
	}

	server := &Server{
		Addr:         addr,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return server, nil
}

func (s *Server) Serve(l net.Listener) error {
	s.m.Lock()

	s.server[tcp] = &customP.Server{
		Listener:     l,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,
	}

	s.m.Unlock()
	return s.server[tcp].ActivateAndServe()
}

func (s *Server) ServePacket(p net.PacketConn) error {
	s.m.Lock()

	s.server[udp] = &customP.Server{
		PacketConn:   p,
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,
	}

	s.m.Unlock()
	return s.server[udp].ActivateAndServe()
}

// TODO:端口复用,SO_REUSEPORT
func (s *Server) Listen() (net.Listener, error) {
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (s *Server) ListenPacket() (net.PacketConn, error) {
	p, err := net.ListenPacket("udp", s.Addr)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (s *Server)     () string { return s.Addr }
