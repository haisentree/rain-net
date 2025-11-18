package custom

// 自定义用于测试的协议,类似于http协议格式
// "GET /ping HTTP/1.1\r\nHost: 43.139.232.236\r\nUser-Agent: python-requests/2.32.3\r\nAccept-Encoding: gzip, deflate\r\nAccept: */*\r\nConnection: keep-alive\r\n\r\n"

type MsgHdr struct {
	header string
}

type Msg struct {
	MsgHdr
	body string
}

type ResponseWriter interface {
	Write([]byte) (int, error)
}
