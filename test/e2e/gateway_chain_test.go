package e2e

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/haisentree/rain-net/config"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/star/starserver"
)

// TestNodeGatewayChain 场景 C(design.md §六):节点串联网关。
// 外部用户 → 节点 A(proxy-in,出站链路 office)══隧道══► 节点 B(star-in 收流)
// → B 按 clientProxy.addr 拨自身内网 echo → 应答原路返回。
// 节点 A 通过 BuildServersFromRoot 从新 schema 装配(role: outbound 内嵌客户端)。
func TestNodeGatewayChain(t *testing.T) {
	// 节点 B:star 服务端 + 自身内网 echo
	backend := startEchoServer(t)
	hubB := starhandler.NewCtrlHub()
	ctrlB := starhandler.NewCtrlProxyServer("nodeB", &config.ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings: config.Settings{
			ClientProxy: []config.ClientProxy{{
				ClientProxyName: "client-0",
				StreamId:        "svc-a",
				Addr:            backend,
				KeyPassword:     "pwd",
			}},
		},
	}, hubB)
	lnB, err := ctrlB.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer lnB.Close()
	go func() { _ = ctrlB.Serve(lnB) }()

	// 节点 A:新 schema 装配(proxy-in + 出站链路)
	rootA := &config.Root{
		Services: []config.ServiceSpec{{
			Name: "nodeA",
			Ingress: []config.IngressSpec{{
				Name:      "proxy-in",
				Transport: "tcp",
				Addr:      "127.0.0.1:0",
			}},
		}},
		Dialers: []config.DialerSpec{{
			Name:        "office",
			Type:        "star",
			Role:        "outbound",
			Addr:        lnB.Addr().String(),
			Identity:    "client-0",
			KeyPassword: "pwd",
		}},
		Routes: []config.RouteSpec{{
			Ingress: "proxy-in",
			To:      "office",
			Streams: []string{"svc-a"},
		}},
	}
	serversA, err := starserver.BuildServersFromRoot(rootA)
	if err != nil {
		t.Fatal(err)
	}
	var proxyLn net.Listener
	for _, srv := range serversA {
		if p, ok := srv.(*starserver.ProxyServer); ok {
			proxyLn, err = p.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer proxyLn.Close()
			go func() { _ = p.Serve(proxyLn) }()
		}
	}
	if proxyLn == nil {
		t.Fatal("node A has no proxy server")
	}

	// 等链路客户端在 B 注册完成
	waitFor(t, 8*time.Second, "chain client registered on B", func() bool {
		return hubB.Streams.Len() == 1
	})

	// 外部用户经 A 穿到 B 的内网 echo
	ext, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ext.Close()
	if err := ext.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("through-nodeA-to-nodeB")
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
