package main

import (
	"bufio"
	"fmt"
	"net"
)

func process(conn net.Conn) {
	//延迟关闭连接
	defer conn.Close()
	for {
		//阅读conn中的内容
		//bufio.NewReader打开一个文件，并返回一个文件句柄
		reader := bufio.NewReader(conn)
		//开一个128字节大小的字符缓冲区
		var buf [128]byte
		//读取reader中的内容放到buf中，n是大小
		n, err := reader.Read(buf[:])
		if err != nil {
			fmt.Println("从客户端读取消息失败..., err", err)
			break
		} else {
			fmt.Println("收到一条数据：")
		}
		recvStr := string(buf[:n])
		fmt.Println(recvStr)
		//回复接收成功
		fmt.Println("向客户端发送确认消息！")
		echo := "echo: " + recvStr
		conn.Write([]byte(echo))
	}
}

func main() {
	//监听
	//服务器开始等待客户端的连接，listen函数不会阻塞
	listen, err := net.Listen("tcp", "127.0.0.1:20000")
	if err != nil {
		fmt.Println("建立连接失败, err", err)
		return
	}
	fmt.Println("准备接收客户端的连接...")
	for {
		//建立连接
		//accept函数从已经建立连接的队列中取出一个连接给客户端程序，进行点对点的连接
		//accept函数会返回一个新的可用的socket
		//accept函数会阻塞
		conn, err := listen.Accept()
		if err != nil {
			fmt.Println("连接失败, err", err)
			continue
		} else {
			fmt.Println("连接成功！")
		}
		go process(conn)
	}
}
