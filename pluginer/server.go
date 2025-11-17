package pluginer

import "net"

type ServerType struct {
	Directives func() []string // 支持的插件名称
}

func RegisterServerType(typeName string, srv ServerType) {
	if _, ok := serverTypes[typeName]; ok {
		panic("server type already registered")
	}
	serverTypes[typeName] = srv
}

type Server interface {
	TCPServer
	UDPServer
}

type TCPServer interface {
	Listen() (net.Listener, error)
	Serve(net.Listener) error
}

type UDPServer interface {
	ListenPacket() (net.PacketConn, error)
	ServePacket(net.PacketConn) error
}

type ServerListener struct {
	server   Server
	listener net.Listener
	packet   net.PacketConn
}
