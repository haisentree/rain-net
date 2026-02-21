package star

import (
	"bufio"
	"net"
)

type response struct {
	pcSession net.Addr //当使用通用的 net.PacketConn 时，存储远程地址，用于写回响应。
	udp       net.PacketConn
	tcp       net.Conn
	reader    *bufio.Reader
	writer    *bufio.Writer

	keepAlive bool
}

func (w *response) LocalAddr() net.Addr {
	switch {
	case w.udp != nil:
		return w.udp.LocalAddr()
	case w.tcp != nil:
		return w.tcp.LocalAddr()
	default:
		panic("dns: internal error: udp and tcp both nil")
	}
}

func (w *response) GetReader() *bufio.Reader {
	return w.reader
}

func (w *response) SetKeepAlive(status bool) {
	w.keepAlive = status
}

// Write implements the ResponseWriter.Write method.
func (w *response) Write(m []byte) (int, error) {
	switch {
	case w.udp != nil:
		return w.udp.WriteTo(m, w.pcSession)
	case w.tcp != nil:
		return w.tcp.Write(m)
	default:
		panic("dns: internal error: udp and tcp both nil")
	}
}
