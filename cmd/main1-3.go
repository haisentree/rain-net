package main

import (
	"fmt"
	"net"
)

func main() {
	socket, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: 20000,
	})
	if err != nil {
		fmt.Println("连接服务器失败！err:", err)
		return
	}
	defer socket.Close()
	sendData := []byte("hello server")
	//发送数据
	_, err = socket.Write(sendData)
	if err != nil {
		fmt.Println("发送数据失败！err:", err)
		return
	}

	//接收数据
	data := make([]byte, 4096)
	n, remoteAddr, err := socket.ReadFromUDP(data)
	if err != nil {
		fmt.Println("接收数据失败！ err:", err)
		return
	}
	fmt.Printf("recv:%v addr:%v count:%v\n\n", string(data[:n]), remoteAddr, n)
}
