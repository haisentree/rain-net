package base

import (
	"flag"
	"fmt"
	"net"
	"strings"
)

// 远程主机: ./udp-server -addr :8989
// 内网主机: ./udp-client -addr 8.148.84.185:8989 -clinetPort 54333

// 内网主机: ./tcp-server -remote :54321
// 远程主机: ./tcp-client -remote 122.188.144.215:51993 -clinetPort 54222

func UdpClientServer() {
	addr2 := flag.String("addr", "0.0.0.0:8080", "监听的主机地址")
	flag.Parse()

	// 1. 创建UDP地址
	addr, err := net.ResolveUDPAddr("udp", *addr2)
	if err != nil {
		fmt.Printf("Failed to resolve address: %v\n", err)
		return
	}

	// 2. 监听UDP端口
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Printf("UDP server listening on %s\n", addr.String())

	// 3. 循环处理客户端消息
	for {
		buffer := make([]byte, 1024)
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
			continue
		}

		msg := string(buffer[:n])
		fmt.Printf("Received from %s: %s\n", clientAddr.String(), msg)

		// 4. 回复客户端（可选）
		response := fmt.Sprintf("Echo: %s", strings.ToUpper(msg))
		_, err = conn.WriteToUDP([]byte(response), clientAddr)
		if err != nil {
			fmt.Printf("Write error: %v\n", err)
		}
	}
}
