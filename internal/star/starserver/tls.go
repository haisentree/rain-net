package starserver

import (
	"crypto/tls"
	"fmt"
	"net"
)

// listenTCP 按监听器设置建立 TCP 监听:settings.tls 为 true 时加载证书
// 并包裹 TLS。隧道加密只影响传输层,star 协议本身不变
func listenTCP(addr string, settings Settings) (net.Listener, error) {
	if !settings.TLS {
		return net.Listen("tcp", addr)
	}

	cert, err := tls.LoadX509KeyPair(settings.CertFile, settings.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls cert: %w", err)
	}
	return tls.Listen("tcp", addr, &tls.Config{Certificates: []tls.Certificate{cert}})
}
