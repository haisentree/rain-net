package base

import (
	"flag"
	"fmt"
	"net"
	"time"
)

func TcpClientStart() {
	remote := flag.String("remote", "0.0.0.0:8083", "监听的主机地址")
	flag.Parse()

	serverAddr := *remote // 目标服务器地址
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}

	conn, err := dialer.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	defer conn.Close()

	// inputReader := bufio.NewReader(os.Stdin)
	for {
		// input, _ := inputReader.ReadString('\n') // 读取用户输入
		// inputInfo := strings.Trim(input, "\r\n")
		// if strings.ToUpper(inputInfo) == "Q" { // 如果输入q就退出
		// 	return
		// }

		_, err = conn.Write([]byte("CONNECT secure.example.com:443 HTTP/1.1\r\nHost: secure.example.com:443\r\nProxy-Connection: Keep-Alive\r\n\r\n")) // 发送数据
		if err != nil {
			return
		}
		buf := [512]byte{}
		n, err := conn.Read(buf[:])
		if err != nil {
			fmt.Println("recv failed, err:", err)
			return
		}
		fmt.Println(string(buf[:n]))
		break
	}
}
