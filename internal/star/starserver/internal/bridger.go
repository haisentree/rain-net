package internal

import "net"

type Bridger struct {
	Name             string
	ListenerProxyMap map[string]ListeneProxyConn
	ClientProxyMap   map[string]ClientProxyConn
}

type ListeneProxyConn struct {
	Name string
	Conn *net.Conn
}

type ClientProxyConn struct {
	Name string
	Conn *net.Conn
}
