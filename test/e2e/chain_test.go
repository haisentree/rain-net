package e2e

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/client"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/protocol/star"
)

// TestNodeChainDialIntranet 节点串联(design.md §六 场景 C 的链路核心):
// 节点 A(链上客户端)通过 DialIntranet 在节点 B 的已注册流上主动建立连接,
// B 按 clientProxy 的 addr 拨自己的内网目标并桥接。
// 半关闭穿透:A 关写 → B 内网连接关写 → EOF 反应式后端应答 → A 收到完整应答。
func TestNodeChainDialIntranet(t *testing.T) {
	backend := startEOFReactiveServer(t, "chain-ok")

	// 节点 B:star 服务端,凭据指向自身内网目标
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

	// 节点 A:链上客户端(等价于 raind 内嵌的 star 出站)
	dialer := client.NewDialer("nodeA", lnB.Addr().String(), "pwd",
		[]star.RegisterStreamReq{{ClientProxyName: "client-0", StreamId: "svc-a"}})
	go func() { _ = dialer.Run() }()
	defer dialer.Close()

	waitFor(t, 5*time.Second, "stream registered on B", func() bool {
		return hubB.Streams.Len() == 1
	})

	cc := dialer.Client()
	if cc == nil {
		t.Fatal("client session nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := cc.DialIntranet(ctx, "client-0", "svc-a")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 请求 + 半关闭,等 EOF 反应式后端的完整应答
	if _, err := conn.Write([]byte("chain-request")); err != nil {
		t.Fatal(err)
	}
	if err := conn.(interface{ CloseWrite() error }).CloseWrite(); err != nil {
		t.Fatalf("CloseWrite through chain: %v", err)
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if string(got) != "chain-ok" {
		t.Fatalf("reply = %q, want %q", got, "chain-ok")
	}
}

// TestNodeChainRejectedUnknownStream 未注册的流必须被拒绝(身份绑定校验)。
func TestNodeChainRejectedUnknownStream(t *testing.T) {
	hubB := starhandler.NewCtrlHub()
	ctrlB := starhandler.NewCtrlProxyServer("nodeB", &config.ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings: config.Settings{
			ClientProxy: []config.ClientProxy{{
				ClientProxyName: "client-0",
				StreamId:        "svc-a",
				Addr:            "127.0.0.1:1",
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

	dialer := client.NewDialer("nodeA", lnB.Addr().String(), "pwd",
		[]star.RegisterStreamReq{{ClientProxyName: "client-0", StreamId: "svc-a"}})
	go func() { _ = dialer.Run() }()
	defer dialer.Close()
	waitFor(t, 5*time.Second, "stream registered", func() bool {
		return hubB.Streams.Len() == 1
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 冒用其他身份的流 → 拒绝
	if _, err := dialer.Client().DialIntranet(ctx, "client-other", "svc-x"); err == nil {
		t.Fatal("expected dial of unregistered identity/stream to be rejected")
	}
}
