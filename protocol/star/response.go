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

func (w *response) GetConn() net.Conn {
	return w.tcp
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

// ClientConnReq 节点串联:客户端请求在已注册流上建立一条连接。
// connID 由客户端分配(高位命名空间),服务端应答后,该连接的
// data/streamClose 帧与既有连接同语义双向流动。
type ClientConnReq struct {
	ConnID          uint32 `json:"connId"`
	ClientProxyName string `json:"clientProxyName"`
	StreamId        string `json:"streamId"`
}

// ClientConnAck 节点串联应答。
type ClientConnAck struct {
	ConnID  uint32 `json:"connId"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// WindowUpdate 流控额度归还:接收方消费 N 字节后告知发送方。
type WindowUpdate struct {
	ConnID uint32 `json:"connId"`
	Delta  uint32 `json:"delta"`
}
