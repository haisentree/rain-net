package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

func main() {
	port := ":20000"
	var wg sync.WaitGroup

	// 启动 TCP 服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		startTCPServer(port)
	}()

	// 启动 UDP 服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		startUDPServer(port)
	}()

	fmt.Printf("服务器启动，在端口 %s 同时监听 TCP 和 UDP...\n", port)
	wg.Wait()
}

func startTCPServer(port string) {
	// 监听 TCP
	tcpListener, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Printf("TCP 监听错误: %v\n", err)
		return
	}
	defer tcpListener.Close()

	fmt.Printf("TCP 服务器监听在 %s\n", port)

	for {
		// 接受 TCP 连接
		conn, err := tcpListener.Accept()
		if err != nil {
			fmt.Printf("TCP 接受连接错误: %v\n", err)
			continue
		}

		// 为每个 TCP 连接创建 goroutine
		go handleTCPConnection(conn)
	}
}

func handleTCPConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	fmt.Printf("TCP 连接来自: %s\n", remoteAddr)

	// 读取数据
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("TCP 读取错误: %v\n", err)
		return
	}

	data := string(buffer[:n])
	fmt.Printf("TCP 收到数据: %s\n", data)

	// 处理并返回数据 (转为大写)
	response := strings.ToUpper(data)
	_, err = conn.Write([]byte(response))
	if err != nil {
		fmt.Printf("TCP 写入错误: %v\n", err)
		return
	}

	fmt.Printf("TCP 发送响应: %s\n", response)
}

func startUDPServer(port string) {
	// 解析 UDP 地址
	udpAddr, err := net.ResolveUDPAddr("udp", port)
	if err != nil {
		fmt.Printf("UDP 地址解析错误: %v\n", err)
		return
	}

	// 监听 UDP
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Printf("UDP 监听错误: %v\n", err)
		return
	}
	defer udpConn.Close()

	fmt.Printf("UDP 服务器监听在 %s\n", port)

	buffer := make([]byte, 1024)

	for {
		// 读取 UDP 数据
		n, remoteAddr, err := udpConn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("UDP 读取错误: %v\n", err)
			continue
		}

		data := string(buffer[:n])
		fmt.Printf("UDP 数据来自: %s, 内容: %s\n", remoteAddr, data)

		// 处理并返回数据 (转为大写)
		response := strings.ToUpper(data)
		_, err = udpConn.WriteToUDP([]byte(response), remoteAddr)
		if err != nil {
			fmt.Printf("UDP 写入错误: %v\n", err)
			continue
		}

		fmt.Printf("UDP 发送响应: %s\n", response)
	}
}
