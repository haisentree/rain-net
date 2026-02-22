package star

import (
	"bufio"
	"net"
)

type ResponseWriter interface {
	LocalAddr() net.Addr
	GetReader() *bufio.Reader
	SetKeepAlive(status bool)
	Write(m []byte) (int, error)
	GetConn() net.Conn
}
