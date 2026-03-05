package internal

import "net"

type Bridger struct {
	Master  Master
	Slavers []Slvater
}

type Master struct {
	Name      string
	Stream    map[string]*net.Conn
	ProxyConn *net.Conn
}

type Slvater struct {
	Name  string
	Port  string
	Conns []*net.Conn
}
