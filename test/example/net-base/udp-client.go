package base

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

func UdpClientStart() {
	addr3 := flag.String("addr", "0.0.0.0:8080", "监听的主机地址")
	clientPort := flag.Int("clinetPort", 54321, "监听的主机地址")
	flag.Parse()

	// 1. 解析服务器地址
	serverAddr, err := net.ResolveUDPAddr("udp", *addr3)
	if err != nil {
		fmt.Printf("Failed to resolve server address: %v\n", err)
		return
	}

	localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", *clientPort))
	if err != nil {
		fmt.Printf("创建本地地址失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 创建本地UDP连接（不绑定特定端口）
	conn, err := net.DialUDP("udp", localAddr, serverAddr)
	if err != nil {
		fmt.Printf("Failed to dial: %v\n", err)
		return
	}
	defer conn.Close()

	a1 := 1
	a2 := 0

	interval := &a1
	count := &a2

	fmt.Printf("UDP客户端启动成功\n")
	fmt.Printf("本地地址: %s\n", conn.LocalAddr().String())
	fmt.Printf("远程地址: %s\n", conn.RemoteAddr().String())
	fmt.Printf("发送间隔: %d秒\n", *interval)
	fmt.Printf("发送次数: ")
	if *count == 0 {
		fmt.Printf("无限\n")
	} else {
		fmt.Printf("%d次\n", *count)
	}
	fmt.Println("------------------------")

	// 3. 每秒发送消息
	sendMessages(conn, *interval, *count)
}

func sendMessages(conn *net.UDPConn, interval, count int) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	sentCount := 0
	for {
		select {
		case <-ticker.C:
			// 准备消息
			message := fmt.Sprintf("Hello UDP Server! 时间: %s 序号: %d",
				time.Now().Format("15:04:05"), sentCount+1)

			// 发送消息
			_, err := conn.Write([]byte(message))
			if err != nil {
				fmt.Printf("发送失败: %v\n", err)
				continue
			}

			sentCount++
			fmt.Printf("[%s] 已发送 #%d: %s\n",
				time.Now().Format("15:04:05"), sentCount, message)

			// 异步接收响应
			go receiveResponse(conn)

			// 检查是否达到发送次数
			if count > 0 && sentCount >= count {
				fmt.Printf("达到发送次数 %d，程序退出\n", count)
				return
			}
		}
	}
}

func receiveResponse(conn *net.UDPConn) {
	// 设置短暂的接收超时
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

	buffer := make([]byte, 1024)
	n, addr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		// 超时是正常的，不打印错误
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return
		}
		fmt.Printf("接收错误: %v\n", err)
		return
	}

	fmt.Printf("  收到来自 %s 的响应: %s\n", addr.String(), string(buffer[:n]))
}
