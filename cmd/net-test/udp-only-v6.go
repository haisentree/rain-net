package main

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

func main() {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
			})
		},
	}

	// 注意：这里使用 "udp6"
	packetConn, err := lc.ListenPacket(context.Background(), "udp6", "[::]:8080")
	if err != nil {
		panic(err)
	}
	defer packetConn.Close()

	fmt.Println("服务器正在仅监听 IPv6 (UDP) :8080")
	buffer := make([]byte, 1024)
	for {
		n, addr, err := packetConn.ReadFrom(buffer)
		if err != nil {
			fmt.Println(err.Error())
			contin
		}
		// ... 处理数据包
		fmt.Println(buffer[0:n])
		fmt.Println(addr)
	}
}
