package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	//开始拨号
	conn, err := net.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		fmt.Println("err: ", err)
		return
	}
	defer conn.Close()
	//打开os.Stdin，并返回一个文件句柄
	inputReader := bufio.NewReader(os.Stdin)
	for {
		//读取用户输入
		fmt.Println("请输入待发送的消息：")
		//输入换行结束输入
		input, _ := inputReader.ReadString('\n')
		//strings.Trim去掉字符串首部和尾部的匹配内容
		inputInfo := strings.Trim(input, "\r\n")
		//如果输入q就退出
		if strings.ToUpper(inputInfo) == "Q" {
			fmt.Println("停止输入，断开连接！")
			return
		}

		//发送数据
		fmt.Println("开始发送数据...")
		_, err = conn.Write([]byte(inputInfo))
		if err != nil {
			return
		} else {
			fmt.Println("发送成功！")
		}

		buf := [512]byte{}
		n, err := conn.Read(buf[:])
		if err != nil {
			fmt.Println("接收失败, err: ", err)
			return
		} else {
			fmt.Println("收到了服务器的回复：")
		}
		fmt.Println(string(buf[:n]))
	}
}
