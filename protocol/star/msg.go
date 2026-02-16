package star

import "net"

type ResponseWriter interface {
	LocalAddr() net.Addr
}
