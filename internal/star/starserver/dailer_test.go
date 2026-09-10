package starserver

import (
	"net"
	"testing"
	"time"

	"rain-net/protocol/star"
)

func TestDailerReconnect(t *testing.T) {
	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Addr:      "127.0.0.1:0",
		Settings: Settings{
			ClientProxy: []ClientProxy{
				{ClientProxyName: "clientProxy-0", KeyPassword: "pwd-0"},
			},
		},
	}, hub)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = ctrl.Serve(ln) }()

	dialer := NewDialer("dailer-0", ln.Addr().String(), "pwd-0",
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}})
	dialer.KeepAlive = 50 * time.Millisecond
	go func() { _ = dialer.Run() }()

	// 首次连接成功
	waitFor(t, 5*time.Second, "first connection registered", func() bool {
		return hub.Streams.Len() == 1
	})
	first := dialer.Client()
	if first == nil {
		t.Fatal("dialer client is nil after connect")
	}

	// 模拟断线:关闭当前会话,dialer 应自动重连并重新注册
	first.Close()
	waitFor(t, 10*time.Second, "reconnection with fresh client", func() bool {
		c := dialer.Client()
		return c != nil && c != first && hub.Streams.Len() == 1
	})

	// 保活长期在线:连接不再变化
	still := dialer.Client()
	time.Sleep(200 * time.Millisecond)
	if dialer.Client() != still {
		t.Fatal("client changed unexpectedly while healthy")
	}

	// Close 后不再重连
	dialer.Close()
	waitFor(t, 5*time.Second, "streams cleaned after close", func() bool {
		return hub.Streams.Len() == 0
	})
	time.Sleep(200 * time.Millisecond)
	if got := hub.Streams.Len(); got != 0 {
		t.Fatalf("stream count = %d after close, want 0", got)
	}
}
