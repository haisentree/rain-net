package custom

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"
)

type Server struct {
	Addr         string
	Net          string
	Listener     net.Listener
	PacketConn   net.PacketConn
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
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

func (srv *Server) serveUDP(l net.PacketConn) error {
	defer l.Close()

	for {
		buffer := make([]byte, 1024)

		n, addr, err := l.ReadFrom(buffer)
		if err != nil {
			return err
		}

		fmt.Printf("Received %s from %s\n", string(buffer[:n]), addr)

		go srv.serveUDPPacket(buffer[:n], l, addr)
	}
}

func (srv *Server) serveTCPConn(conn net.Conn) {
	//延迟关闭连接
	// defer conn.Close()
	for {
		//阅读conn中的内容
		//bufio.NewReader打开一个文件，并返回一个文件句柄
		reader := bufio.NewReader(conn)
		//开一个128字节大小的字符缓冲区
		var buf [128]byte
		//读取reader中的内容放到buf中，n是大小
		n, err := reader.Read(buf[:])
		if err != nil {
			fmt.Println("从客户端读取消息失败..., err", err)
			break
		} else {
			fmt.Println("收到一条数据：")
		}
		recvStr := string(buf[:n])
		fmt.Println(recvStr)
		//回复接收成功
		fmt.Println("向客户端发送确认消息！")
		echo := "echo: " + recvStr
		conn.Write([]byte(echo))
	}
}

func (srv *Server) serveUDPPacket(m []byte, u net.PacketConn, addr net.Addr) {
	_, err := u.WriteTo(m, addr)
	fmt.Println("123334445")
	if err != nil {
		fmt.Println("serveUDPPacket err:", err.Error())
	}
}
