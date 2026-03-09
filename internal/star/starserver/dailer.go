package starserver

import (
	"net"
	"time"
)

type Dialer struct {
	Name      string
	Addr      string
	Transport string

	KeyPassword     string
	ClientProxyName string
}

func NewDler() *Dialer {
	return &Dialer{}
}

func (d *Dialer) Dial() (net.Conn, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	return dialer.Dial("tcp", d.Addr)
}

func (d *Dialer) HandleDail(conn net.Conn) error {
	return nil
}

func (d *Dialer) DialPacket() (net.PacketConn, error) {
	return net.ListenPacket("udp", d.Addr)
}

func (d *Dialer) HandleDialPacket(net.PacketConn) error {
	return nil
}
