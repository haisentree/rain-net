package customserver

import (
	"errors"
	"net"
	"sync"
	"time"

	custom "rain-net/protocol/custom"
)

// custom启动指定端口的
type Server struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	ServiceCfg   *Service

	serverMap map[string]*custom.Server // 0 is a net.Listener, 1 is a net.PacketConn (a *UDPConn) in our case.
	m         sync.Mutex                // protects the servers
}

func NewServer(srv *Service) (*Server, error) {
	if len(srv.Host) == 0 {
		return nil, errors.New("node is empty")
	}

	server := &Server{
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
		ServiceCfg:   srv,
		serverMap:    make(map[string]*custom.Server),
	}

	return server, nil
}

func (s *Server) Serve(l net.Listener) error {
	s.m.Lock()
	if s.ServiceCfg == nil {
		return errors.New("Serve is empty")
	}
	for _, val := range s.ServiceCfg.Host {
		switch val.Network {
		case "tcp", "tpc6":
			s.serverMap[val.Key] = &custom.Server{
				Listener:     l,
				ReadTimeout:  s.ReadTimeout,
				WriteTimeout: s.WriteTimeout,
			}
		}
	}

	s.m.Unlock()
	return s.server[tcp].ActivateAndServe()
}

// func (s *Server) ServePacket(p net.PacketConn) error {
// 	s.m.Lock()

// 	s.server[udp] = &custom.Server{
// 		PacketConn:   p,
// 		ReadTimeout:  s.ReadTimeout,
// 		WriteTimeout: s.WriteTimeout,
// 	}

// 	s.m.Unlock()
// 	return s.server[udp].ActivateAndServe()
// }

// // TODO:端口复用,SO_REUSEPORT
// func (s *Server) Listen() (net.Listener, error) {
// 	l, err := net.Listen("tcp", s.Addr)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return l, nil
// }

// func (s *Server) ListenPacket() (net.PacketConn, error) {
// 	p, err := net.ListenPacket("udp", s.Addr)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return p, nil
// }

// func (s *Server)     () string { return s.Addr }
