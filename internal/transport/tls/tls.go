// Package tls 在 TCP 之上叠加 TLS:入站做 TLS 终止,出站做 TLS 客户端封装。
//
// 帧格式不变,半关闭经 tls.Conn 的 CloseWrite(close_notify)传播。
package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/haisentree/rain-net/internal/transport"
)

type tlsTransport struct {
	cert       *tls.Certificate // 服务端证书;出站可为 nil
	skipVerify bool             // 出站跳过证书校验(自签名场景)
}

func (t *tlsTransport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	if t.cert == nil {
		return nil, fmt.Errorf("tls: listen requires certFile/keyFile")
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{*t.cert}, MinVersion: tls.VersionTLS12}
	return transport.WatchClose(ctx, tls.NewListener(ln, cfg)), nil
}

func (t *tlsTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: t.skipVerify, // 自签名场景;生产配置应提供受信 CA
		MinVersion:         tls.VersionTLS12,
	}
	return tls.Client(raw, cfg), nil
}

func init() {
	transport.Register("tls", build)
}

func build(opts transport.Options) (transport.Transport, error) {
	t := &tlsTransport{skipVerify: opts.TLSSkipVerify}
	if opts.CertFile != "" || opts.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls: load key pair: %w", err)
		}
		t.cert = &cert
	}
	return t, nil
}
