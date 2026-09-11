package e2e

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/client"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/star/starserver"
	"github.com/haisentree/rain-net/internal/transport"
	"github.com/haisentree/rain-net/protocol/star"
)

// TestTransportMatrix 传输转换矩阵(docs/design.md §六 场景 B/C):
// 入站传输 {tcp,tls,ws,h2,quic} × ctrl 通道传输 {tcp,tls} 的隧道全链路。
// 同一条"外部请求 → 代理 → 隧道 → 内网 echo"路径,只换两端传输实现——
// 证明"传输正交、协议透传"的模型承诺。
func TestTransportMatrix(t *testing.T) {
	ingressTransports := []string{"tcp", "tls", "ws", "h2", "quic"}
	ctrlTransports := []string{"tcp", "tls"}

	for _, ingressTr := range ingressTransports {
		for _, ctrlTr := range ctrlTransports {
			name := ingressTr + "_over_" + ctrlTr
			t.Run(name, func(t *testing.T) {
				runMatrixCase(t, ingressTr, ctrlTr)
			})
		}
	}
}

func runMatrixCase(t *testing.T, ingressTr, ctrlTr string) {
	backend := startEchoServer(t)

	hub := starhandler.NewCtrlHub()

	// ctrl 通道(tcp/tls)
	ctrlSettings := config.Settings{
		ClientProxy: []config.ClientProxy{
			{ClientProxyName: "clientProxy-0", KeyPassword: "pwd"},
		},
	}
	if ctrlTr == "tls" {
		certFile, keyFile := genSelfSignedCert(t)
		ctrlSettings.TLS = true
		ctrlSettings.CertFile = certFile
		ctrlSettings.KeyFile = keyFile
	}
	ctrl := starhandler.NewCtrlProxyServer("svc", &config.ListenerList{
		Type:      "ctrlproxy",
		Transport: ctrlTr,
		Settings:  ctrlSettings,
	}, hub)
	// ctrl.Listen 内部按 settings 应用 TLS(裸 net.Listen 会绕过加密)
	ctrlLn, err := ctrl.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

	// 隧道客户端:按 ctrl 通道传输选择 TLS
	dialer := client.NewDialer("dailer-0", ctrlLn.Addr().String(), "pwd",
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}})
	dialer.TLS = ctrlTr == "tls"
	dialer.TLSSkipVerify = true
	dialer.LocalDialer = func(streamId string) (net.Conn, error) {
		return net.Dial("tcp", backend)
	}
	go func() { _ = dialer.Run() }()
	defer dialer.Close()

	// proxy 入站(任意传输)
	ingressSettings := config.Settings{ProxyName: "p0"}
	if ingressTr == "tls" || ingressTr == "quic" {
		certFile, keyFile := genSelfSignedCert(t)
		ingressSettings.CertFile = certFile
		ingressSettings.KeyFile = keyFile
	}
	connects := starserver.NewConnectTable()
	connects.Seed([]config.ConnectItem{{
		ProxyName: "p0", ClientProxyName: "clientProxy-0", StreamId: "stream-0",
	}})
	proxy, perr := starserver.NewProxyServer("svc", &config.ListenerList{
		Type:      "proxy",
		Transport: ingressTr,
		Settings:  ingressSettings,
	}, connects, hub)
	if perr != nil {
		t.Fatal(perr)
	}
	ln, err := proxy.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = proxy.Serve(ln) }()

	// 等流注册
	waitFor(t, 5*time.Second, "stream registered", func() bool {
		return hub.Streams.Len() == 1
	})

	// 外部用户经同传输拨入,回环穿过隧道
	extTr, err := transport.Build(ingressTr, transport.Options{TLSSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	ext, err := extTr.Dial(dctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ext.Close()
	if err := ext.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	payload := []byte("matrix:" + ingressTr + "/" + ctrlTr)
	if _, err := ext.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(ext, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("echo mismatch: %q", buf)
	}
	_ = tls.VersionTLS12 // 保持 tls import(证书场景由 genCert 覆盖)
}
