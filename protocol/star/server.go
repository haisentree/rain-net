package star

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"
)

type Handler interface {
	ServeStar(w ResponseWriter, data []byte)
}

type HandlerFunc func(w ResponseWriter, data []byte)

func (f HandlerFunc) ServeStar(w ResponseWriter, data []byte) {
	f(w, data)
}

type Server struct {
	Addr         string
	Net          string
	Listener     net.Listener
	PacketConn   net.PacketConn
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	Handler Handler
}

func (srv *Server) init() {
	fmt.Println("[Server.init]")
}

func (srv *Server) ListenAndServe() error {

	srv.init()
	switch srv.Net {
	case "tcp", "tcp4", "tcp6":
		l, err := net.Listen(srv.Net, srv.Addr)
		if err != nil {
			return err
		}
		srv.Listener = l
		return srv.serveTCP(l)
	case "udp", "udp4", "udp6":
		l, err := net.ListenPacket(srv.Net, srv.Addr)
		if err != nil {
			return err
		}
		srv.PacketConn = l
		return srv.serveUDP(l)
	}
	return errors.New("bad network")
}

func (srv *Server) ActivateAndServe() error {
	if srv.PacketConn != nil {
		srv.serveUDP(srv.PacketConn)
	}
	if srv.Listener != nil {
		return srv.serveTCP(srv.Listener)
	}
	return nil
}

func (srv *Server) serveTCP(l net.Listener) error {
	defer l.Close()

	for {
		rw, err := l.Accept()
		if err != nil {
			return err
		}
		go srv.serveTCPConn(rw)
	}
}

func (srv *Server) serveUDP(p net.PacketConn) error {
	defer p.Close()

	for {
		srv.serveUDPPacket(p)
	}
}

func (srv *Server) serveTCPConn(conn net.Conn) {
	// reader := bufio.NewReader(conn)
	w := &response{
		tcp:    conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),

		keepAlive: false, // 防止使用没有调用read的handle,导致死循环
	}
	buffer := make([]byte, 1024)

	for {

		// n, err := reader.Read(buffer[:])
		// if err != nil {
		// 	fmt.Println("从客户端读取消息失败..., err", err)
		// 	break
		// }
		// fmt.Printf("Received %s \n", string(buffer[:n]))

		// handle中read数据,buffer弃用
		srv.ServeStar(buffer[:], w)
		if !w.keepAlive {
			break
		}
	}
}

func (srv *Server) serveUDPPacket(u net.PacketConn) {
	buffer := make([]byte, 1024)
	w := &response{udp: u}

	n, addr, err := u.ReadFrom(buffer)
	w.pcSession = addr
	if err != nil {
		fmt.Println("serveUDPPacket err:", err.Error())
		return
	}
	fmt.Printf("Received %s from %s\n", string(buffer[:n]), addr)

	go srv.ServeStar(buffer[:n], w)
}

func (srv *Server) ServeStar(m []byte, w *response) {
	// w.udp.WriteTo([]byte("echo"), w.pcSession)
	// w.tcp.Write([]byte("echo"))
	srv.Handler.ServeStar(w, m)
}
