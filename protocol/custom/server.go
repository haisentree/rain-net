package custom

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"
)

type Handler interface {
	ServeCustom(w ResponseWriter, r *Msg)
}

type HandlerFunc func(ResponseWriter, *Msg)

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
		// go srv.serveUDPPacket(buffer[:n], l, addr)  // 卡死
		srv.serveUDPPacket(p)
	}
}

func (srv *Server) serveTCPConn(conn net.Conn) {
	//延迟关闭连接
	// defer conn.Close()
	for {
		reader := bufio.NewReader(conn)
		w := &response{tcp: conn}

		buffer := make([]byte, 1024)
		//读取reader中的内容放到buf中，n是大小
		n, err := reader.Read(buffer[:])
		if err != nil {
			fmt.Println("从客户端读取消息失败..., err", err)
			break
		}
		fmt.Printf("Received %s \n", string(buffer[:n]))
		srv.serveCustom(buffer[:n], w)
		// conn.Write([]byte(echo))
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

	// _, err := u.WriteTo(m, addr)
	go srv.serveCustom(buffer[:n], w)
}

func (srv *Server) serveCustom(m []byte, w *response) {
	// 解析包
	req := &Msg{}
	req.Body = string(m)
	fmt.Println("当前信息:", req.Body)
	w.udp.WriteTo([]byte("echo"), w.pcSession)
	// srv.Handler.ServeCustom(w, req)
}
