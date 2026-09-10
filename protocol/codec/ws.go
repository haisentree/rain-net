package codec

import (
	"net"
	"time"

	"rain-net/protocol/ws"
)

// wsCodec WebSocket 适配器:入站做服务端升级,出站做客户端握手
type wsCodec struct{}

func (wsCodec) Name() string { return "ws" }

func (wsCodec) Accept(conn net.Conn) (MessageConn, error) {
	return ws.Accept(conn)
}

func (wsCodec) Dial(addr string, timeout time.Duration) (MessageConn, error) {
	return ws.Dial(addr, timeout)
}

func init() {
	Register("ws", wsCodec{})
}
