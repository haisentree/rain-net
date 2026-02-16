package base

import (
	"flag"
	"fmt"
	"net"
)

// 远程主机: ./tcp-server -remote :8989
// 内网主机: ./tcp-client -remote 8.148.84.185:8989 -clinetPort 54321

// 内网主机: ./tcp-server -remote :54321
// 远程主机: ./tcp-client -remote 122.188.144.215:51993 -clinetPort 54222

func TcpServerStart() {
	port := flag.String("remote", ":8080", "监听的主机地址")
	flag.Parse()

	listener, err := net.Listen("tcp", *port)
	if err != nil {
		fmt.Println("Error listening:", err)
		return
	}
	defer listener.Close()
	fmt.Println("Server listening on:", *port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	clientAddr := conn.RemoteAddr().String()
	fmt.Printf("Client connected from: %s\n", clientAddr)

	// 向客户端发送欢迎消息
	conn.Write([]byte("Hello from server!\n"))
}
