// Package tcp 提供明文 TCP 传输。
package tcp

import (
	"context"
	"net"

	"github.com/haisentree/rain-net/internal/transport"
)

type tcpTransport struct{}

func (tcpTransport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return transport.WatchClose(ctx, ln), nil
}

func (tcpTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func init() {
	transport.Register("tcp", func(transport.Options) (transport.Transport, error) {
		return tcpTransport{}, nil
	})
}
