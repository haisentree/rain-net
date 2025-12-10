package main

import (
	"fmt"
	"net"
)

func main() {
	udpAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:8081")
	if err != nil {
		fmt.Printf("UDP 地址解析错误: %v\n", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		fmt.Printf("UDP 连接错误: %v\n", err)
		return
	}
	defer conn.Close()

	message := "Hello from UDP Client"
	fmt.Printf("发送: %s\n", message)

	_, err = conn.Write([]byte(message))
	if err != nil {
		fmt.Printf("UDP 发送错误: %v\n", err)
		return
	}

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("UDP 接收错误: %v\n", err)
		return
	}

	fmt.Printf("收到响应: %s\n", string(buffer[:n]))
}
