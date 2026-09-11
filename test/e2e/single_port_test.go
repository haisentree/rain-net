package e2e

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/client"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/ingress/sniff"
	"github.com/haisentree/rain-net/internal/star/starserver"
	"github.com/haisentree/rain-net/protocol/star"
)

// TestSinglePortSniff 单端口多服务(R7 尾巴,docs/research/nps.md §2.1 的落地):
// 一个物理端口同时承载 admin(HTTP)/ star 隧道(魔数前缀)/ proxy 数据面(默认兜底)。
// 组件经 UseListener 注入嗅探分发器的虚拟监听器,业务零改动。
func TestSinglePortSniff(t *testing.T) {
	backend := startEchoServer(t)

	hub := starhandler.NewCtrlHub()

	// 1) 基础监听 + 嗅探分发器
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := sniff.New(context.Background(), base, sniff.Config{
		HTTPDefault: "admin",                                              // HTTP 流量 → admin(HTTPDefault)
		Prefixes:    []sniff.PrefixRule{{Prefix: "0101", Target: "star"}}, // star 帧头(version+handshake)
		Default:     "proxy",                                              // 其余 → proxy 数据面
	})
	if err != nil {
		t.Fatal(err)
	}
	adminLn, _ := dispatcher.Listener("admin")
	starLn, _ := dispatcher.Listener("star")
	proxyLn, _ := dispatcher.Listener("proxy")

	// 2) 组件注入虚拟监听器
	ctrlSettings := config.Settings{
		ClientProxy: []config.ClientProxy{{
			ClientProxyName: "clientProxy-0", StreamId: "stream-0", KeyPassword: "pwd",
		}},
	}
	ctrl := starhandler.NewCtrlProxyServer("svc", &config.ListenerList{
		Type: "ctrlproxy", Transport: "tcp", Settings: ctrlSettings,
	}, hub).UseListener(starLn)

	connects := starserver.NewConnectTable()
	connects.Seed([]config.ConnectItem{{ProxyName: "p0", ClientProxyName: "clientProxy-0", StreamId: "stream-0"}})
	proxy, perr := starserver.NewProxyServer("svc", &config.ListenerList{
		Type: "proxy", Transport: "tcp",
		Settings: config.Settings{ProxyName: "p0"},
	}, connects, hub)
	if perr != nil {
		t.Fatal(perr)
	}
	proxy.UseListener(proxyLn)

	admin := starserver.NewAdminServer("svc", &config.ListenerList{
		Addr:     "127.0.0.1:0",
		Settings: config.Settings{AdminPassword: "secret"},
	}, hub, connects, []*starhandler.CtrlProxyServer{ctrl}).UseListener(adminLn)

	go func() { _ = ctrl.Serve(starLn) }()
	go func() { _ = proxy.Serve(proxyLn) }()
	go func() { _ = admin.Serve(adminLn) }()

	// 3) star 隧道客户端从同一端口接入
	dialer := client.NewDialer("dailer-0", base.Addr().String(), "pwd",
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}})
	dialer.LocalDialer = func(streamId string) (net.Conn, error) {
		return net.Dial("tcp", backend)
	}
	go func() { _ = dialer.Run() }()
	defer dialer.Close()
	waitFor(t, 5*time.Second, "dailer registered via sniff port", func() bool {
		return hub.Streams.Len() == 1
	})

	// 4) admin HTTP 同端口可达(无 token → 401)
	resp, err := http.Get("http://" + base.Addr().String() + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin status = %d, want 401", resp.StatusCode)
	}

	// 5) proxy 数据面同端口回环(经隧道)
	ext, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ext.Close()
	if err := ext.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("single-port-echo")
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
}
