package star

import (
	"net"
	"time"
)

type ClientDialer struct {
	Net string

	addr string

	// 可选配置，例如超时时间
	timeout time.Duration
	// 其他拨号选项...
}

func (d *ClientDialer) Dial() (net.Conn, error) {
	// 使用 net.Dialer 实际建立 TCP 连接
	dialer := net.Dialer{Timeout: d.timeout}
	return dialer.Dial("tcp", d.addr)
}

func (d *ClientDialer) DialPacket() (net.PacketConn, error) {
	// 对于 UDP，返回 net.PacketConn
	return net.ListenPacket("udp", "") // 客户端监听随机端口
	// 如果需要连接到特定远端，可使用 net.Dial("udp", addr) 返回 net.Conn
}
