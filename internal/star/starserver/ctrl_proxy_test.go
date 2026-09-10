package starserver

import (
	"net"
	"testing"
	"time"

	"rain-net/protocol/star"
)

// waitFor 轮询等待条件成立,超时则失败
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", what)
}

func TestStreamTableBasics(t *testing.T) {
	table := NewStreamTable()
	c1, _ := net.Pipe()
	defer c1.Close()
	c2, _ := net.Pipe()
	defer c2.Close()
	sess1 := star.NewSession(c1)
	sess2 := star.NewSession(c2)

	id1, err := table.Assign(sess1, star.RegisterStreamReq{ClientProxyName: "clientProxy-0", StreamId: "stream-0"})
	if err != nil || id1 != 1 {
		t.Fatalf("first assign = %d, %v; want 1, nil", id1, err)
	}
	id2, err := table.Assign(sess2, star.RegisterStreamReq{ClientProxyName: "clientProxy-0", StreamId: "stream-1"})
	if err != nil || id2 != 2 {
		t.Fatalf("second assign = %d, %v; want 2, nil", id2, err)
	}

	// 同一 clientProxyName 下重复 streamId 应被拒绝
	if _, err := table.Assign(sess1, star.RegisterStreamReq{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}); err == nil {
		t.Fatal("expected duplicate stream rejection")
	}
	// 不同 clientProxyName 下同名 streamId 允许
	id3, err := table.Assign(sess2, star.RegisterStreamReq{ClientProxyName: "clientProxy-1", StreamId: "stream-0"})
	if err != nil || id3 != 3 {
		t.Fatalf("assign under other proxy = %d, %v; want 3, nil", id3, err)
	}

	entry, ok := table.Get(1)
	if !ok || entry.StreamId != "stream-0" || entry.Client != sess1 {
		t.Fatalf("Get(1) = %+v, %v", entry, ok)
	}

	if e, ok := table.GetByName("clientProxy-0", "stream-1"); !ok || e.AssignID != 2 {
		t.Fatalf("GetByName = %+v, %v", e, ok)
	}

	if got := table.FindByClientProxy("clientProxy-0"); len(got) != 2 || got[0].AssignID != 1 {
		t.Fatalf("FindByClientProxy(clientProxy-0) = %+v", got)
	}

	table.Remove(1)
	if _, ok := table.Get(1); ok {
		t.Fatal("stream 1 should be removed")
	}

	if n := table.RemoveByClient(sess2); n != 2 {
		t.Fatalf("RemoveByClient(sess2) = %d, want 2", n)
	}
	if table.Len() != 0 {
		t.Fatalf("table len = %d, want 0", table.Len())
	}
}

func TestCtrlProxyCredentials(t *testing.T) {
	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("t1", &ListenerList{
		Settings: Settings{
			KeyPassword: "admin-pwd", // 管理员密码,注册不限身份
			ClientProxy: []ClientProxy{
				{ClientProxyName: "clientProxy-0", KeyPassword: "pwd-0"},
				{ClientProxyName: "clientProxy-1", KeyPassword: "pwd-1"},
			},
		},
	}, hub)

	creds := ctrl.creds()
	if creds["pwd-0"] != "clientProxy-0" || creds["pwd-1"] != "clientProxy-1" {
		t.Fatalf("client creds = %v", creds)
	}
	if v, ok := creds["admin-pwd"]; !ok || v != "" {
		t.Fatalf("admin cred = %q, %v", v, ok)
	}
}

func TestCtrlProxyIdentityBinding(t *testing.T) {
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

	// 正确凭据:注册自己身份名下的流成功
	client, err := star.DialCtrl("tcp", ln.Addr().String(),
		star.HandshakeReq{ClientName: "dailer-0", KeyPassword: "pwd-0"},
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-a"}},
		3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl own identity: %v", err)
	}
	defer client.Close()
	waitFor(t, 3*time.Second, "stream registered", func() bool {
		return hub.Streams.Len() == 1
	})

	// 同一凭据试图注册别人的 clientProxyName:应被拒绝
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	sess := star.NewSession(conn)
	if _, err := star.ClientHandshake(sess, star.HandshakeReq{ClientName: "impostor", KeyPassword: "pwd-0"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	ack, err := star.ClientRegisterStream(sess, star.RegisterStreamReq{ClientProxyName: "clientProxy-1", StreamId: "stream-b"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if ack.OK {
		t.Fatal("expected registration under foreign clientProxyName to be rejected")
	}
	waitFor(t, 3*time.Second, "no extra stream", func() bool {
		return hub.Streams.Len() == 1
	})
}

func TestCtrlProxyEndToEnd(t *testing.T) {
	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("test-1", &ListenerList{
		Name:      "listener-ctrl",
		Type:      "ctrlproxy",
		Transport: "tcp",
		Addr:      "127.0.0.1:0",
		Settings:  Settings{KeyPassword: "pwd-e2e"},
	}, hub)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = ctrl.Serve(ln) }()

	client, err := star.DialCtrl("tcp", ln.Addr().String(),
		star.HandshakeReq{ClientName: "dailer-0", KeyPassword: "pwd-e2e"},
		[]star.RegisterStreamReq{
			{ClientProxyName: "clientProxy-0", StreamId: "dailer-0-stream-1"},
			{ClientProxyName: "clientProxy-0", StreamId: "dailer-0-stream-2"},
		}, 3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl: %v", err)
	}

	id1, ok := client.StreamID("dailer-0-stream-1")
	if !ok || id1 != 1 {
		t.Fatalf("dailer-0-stream-1 -> %d, %v; want 1, true", id1, ok)
	}
	id2, _ := client.StreamID("dailer-0-stream-2")

	// 注册完成后服务端流表立即可查
	if entry, ok := hub.Streams.Get(id1); !ok || entry.StreamId != "dailer-0-stream-1" {
		t.Fatalf("server table entry = %+v, %v", entry, ok)
	}

	go func() { _ = client.Run() }()

	// 保活若干周期后流表仍然完好
	client.StartKeepAlive(10 * time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if hub.Streams.Len() != 2 {
		t.Fatalf("stream len after keepalive = %d, want 2", hub.Streams.Len())
	}

	// 客户端主动关流,服务端流表移除
	if err := client.CloseStream(id1); err != nil {
		t.Fatalf("CloseStream: %v", err)
	}
	waitFor(t, 3*time.Second, "stream removal", func() bool {
		_, ok := hub.Streams.Get(id1)
		return !ok
	})

	// 客户端断开,服务端清理其名下所有流
	client.Close()
	waitFor(t, 3*time.Second, "session cleanup", func() bool {
		return hub.Streams.Len() == 0
	})
	_ = id2
}

func TestCtrlProxyBadPassword(t *testing.T) {
	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("test-1", &ListenerList{
		Name:      "listener-ctrl",
		Type:      "ctrlproxy",
		Transport: "tcp",
		Addr:      "127.0.0.1:0",
		Settings:  Settings{KeyPassword: "right-password"},
	}, hub)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = ctrl.Serve(ln) }()

	_, err = star.DialCtrl("tcp", ln.Addr().String(),
		star.HandshakeReq{ClientName: "intruder", KeyPassword: "wrong-password"},
		nil, 3*time.Second)
	if err == nil {
		t.Fatal("expected DialCtrl error for wrong password")
	}
}
