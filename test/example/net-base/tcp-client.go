package base

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"
)

func TcpClientStart() {
	remote := flag.String("remote", "0.0.0.0:8080", "监听的主机地址")
	clientPort := flag.Int("clinetPort", 54321, "监听的主机地址")
	flag.Parse()

	serverAddr := *remote    // 目标服务器地址
	localPort := *clientPort // 你想要绑定的客户端本地端口

	// 1. 创建一个 Dialer，并配置本地地址和超时
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{
			Port: localPort, // 关键：指定本地端口
		},
		Timeout: 5 * time.Second,
	}

	// 2. 使用 Dialer 建立连接
	fmt.Printf("Connecting to %s from local port %d...\n", serverAddr, localPort)
	conn, err := dialer.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	defer conn.Close()

	fmt.Printf("Connected! Local address: %s\n", conn.LocalAddr())

	// 3. 读取服务器响应
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Println("Error reading:", err)
		return
	}
	fmt.Printf("Server says: %s", string(buffer[:n]))

	// 在连接成功后启动心跳
	go keepAlive(conn, 30*time.Second)

	time.Sleep(100 * time.Second)
}

func keepAlive(conn net.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		_, err := conn.Write([]byte("PING\n"))
		if err != nil {
			log.Println("心跳失败，连接可能已断开:", err)
			return
		}
	}
}
