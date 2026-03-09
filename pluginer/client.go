package pluginer

import "net"

type Client interface {
	TCPClient
	UDPClient
}

type TCPClient interface {
	Dial() (net.Conn, error)
	HandleDail(conn net.Conn) error
}

type UDPClient interface {
	DialPacket() (net.PacketConn, error)
	HandleDialPacket(net.PacketConn) error
}
