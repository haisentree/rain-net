package main

import (
	"fmt"
	"net"
	"time"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()
	buffer := make([]byte, 32*1024) // 32KB 缓冲区
	totalBytes := int64(0)
	start := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			elapsed := time.Since(start).Seconds()
			speed := float64(totalBytes) / elapsed / 1024 // KB/s
			fmt.Printf("\r接收速度: %.2f KB/s, 总计: %d KB", speed, totalBytes/1024)
		}
	}()

	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			totalBytes += int64(n)
		}
		if err != nil {
			break
		}
	}
	fmt.Println("\n连接关闭")
}

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	fmt.Println("TCP 服务器监听 :8080")
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn)
	}
}
