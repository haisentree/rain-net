package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Service []Service `yaml:"service"`
}

type Service struct {
	Name string `yaml:"name"`
	Host []Host `yaml:"host"`
}

type Host struct {
	Network string `yaml:"network"`
	Address string `yaml:"address"`
}

func main() {
	data, err := os.ReadFile("./../../etc/config.sample.yaml")
	if err != nil {
		panic(err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("config:", config)
}

func StartUDPServer(addr string) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		fmt.Println("解析地址失败:", err)
		return
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	defer conn.Close()

	fmt.Printf("监听所有地址 (IPv4和IPv6): %s\n", udpAddr.String())

	buffer := make([]byte, 1024)

	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("读取数据失败:", err)
			continue
		}

		// 检查客户端使用的是IPv4还是IPv6
		if clientAddr.IP.To4() != nil {
			fmt.Printf("IPv4客户端: %s\n", clientAddr.String())
		} else {
			fmt.Printf("IPv6客户端: %s\n", clientAddr.String())
		}

		fmt.Printf("消息: %s\n", string(buffer[:n]))

		// 发送响应
		response := []byte("Hello from server")
		conn.WriteToUDP(response, clientAddr)
	}
}

func StartUDPv6Server(addr string) {

	// 解析UDP地址
	udpAddr, err := net.ResolveUDPAddr("udp6", addr)
	if err != nil {
		fmt.Println("解析地址失败:", err)
		return
	}

	// 创建UDP连接
	conn, err := net.ListenUDP("udp6", udpAddr)
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	defer conn.Close()

	fmt.Printf("监听IPv6 UDP地址: %s\n", udpAddr.String())

	buffer := make([]byte, 1024)

	for {
		// 读取数据
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("读取数据失败:", err)
			continue
		}

		fmt.Printf("收到来自 %s 的消息: %s\n",
			clientAddr.String(),
			string(buffer[:n]))

		// 回应客户端
		response := []byte("已收到消息: " + string(buffer[:n]))
		_, err = conn.WriteToUDP(response, clientAddr)
		if err != nil {
			fmt.Println("发送响应失败:", err)
		}
	}
}

func StartTCPServer() {
	// 监听所有地址（IPv4和IPv6）
	addr := ":8080"

	// 使用 "tcp" 而不是 "tcp4" 或 "tcp6" 可以自动支持双栈（如果系统支持）
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	defer listener.Close()

	fmt.Printf("监听所有地址 (IPv4和IPv6) TCP: %s\n", listener.Addr().String())

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("接受连接失败:", err)
			continue
		}

		// 检查客户端地址是IPv4还是IPv6
		clientAddr := conn.RemoteAddr()
		tcpAddr, ok := clientAddr.(*net.TCPAddr)
		if ok {
			if tcpAddr.IP.To4() != nil {
				fmt.Printf("IPv4客户端: %s\n", clientAddr.String())
			} else {
				fmt.Printf("IPv6客户端: %s\n", clientAddr.String())
			}
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	// 获取客户端地址
	clientAddr := conn.RemoteAddr().String()
	fmt.Printf("客户端连接: %s\n", clientAddr)

	// 读取数据
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("从 %s 读取数据失败: %v\n", clientAddr, err)
		return
	}

	fmt.Printf("收到来自 %s 的消息: %s\n", clientAddr, string(buffer[:n]))

	// 发送响应
	response := []byte("Hello from IPv6 TCP server\n")
	_, err = conn.Write(response)
	if err != nil {
		fmt.Printf("发送响应给 %s 失败: %v\n", clientAddr, err)
	}
}

func StartTCPv6Server() {
	// 解析TCP地址
	tcpAddr, err := net.ResolveTCPAddr("tcp6", "[::]:8080")
	if err != nil {
		fmt.Println("解析地址失败:", err)
		return
	}

	// 监听TCP地址
	listener, err := net.ListenTCP("tcp6", tcpAddr)
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	defer listener.Close()

	fmt.Printf("监听地址: %s\n", listener.Addr().String())

	// 设置接受连接超时
	listener.SetDeadline(time.Now().Add(30 * time.Second))

	for {
		conn, err := listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Println("接受连接超时，重置超时时间")
				listener.SetDeadline(time.Now().Add(30 * time.Second))
				continue
			}
			fmt.Println("接受连接失败:", err)
			continue
		}

		// 重置监听器的超时时间
		listener.SetDeadline(time.Now().Add(30 * time.Second))

		go handleConnectionWithTimeout(conn)
	}
}

func handleConnectionWithTimeout(conn net.Conn) {
	defer conn.Close()

	// 设置连接超时
	conn.SetDeadline(time.Now().Add(60 * time.Second))

	clientAddr := conn.RemoteAddr().String()
	fmt.Printf("客户端连接: %s\n", clientAddr)

	// 读写逻辑...
}
