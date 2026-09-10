package star

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

// startTestCtrlTCP 起一个真实 TCP 监听 + testCtrlServer,返回服务端会话通道和地址
// 客户端握手完成后服务端会话一定已就绪,从通道读取不会阻塞
func startTestCtrlTCP(t *testing.T, password string) (chan *testCtrlServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	tsCh := make(chan *testCtrlServer, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		ts := newTestCtrlServer(conn)
		tsCh <- ts
		_ = ts.serve(password)
	}()
	return tsCh, ln.Addr().String()
}

func TestDialCtrlAndDataFlow(t *testing.T) {
	tsCh, addr := startTestCtrlTCP(t, "pwd")

	client, err := DialCtrl("tcp", addr,
		HandshakeReq{ClientName: "dailer-x", KeyPassword: "pwd"},
		[]RegisterStreamReq{
			{ClientProxyName: "clientProxy-0", StreamId: "stream-0"},
			{ClientProxyName: "clientProxy-1", StreamId: "stream-1"},
		}, 3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl: %v", err)
	}
	defer client.Close()

	if id, ok := client.StreamID("stream-0"); !ok || id != 1 {
		t.Fatalf("stream-0 -> %d, %v; want 1, true", id, ok)
	}
	if id, ok := client.StreamID("stream-1"); !ok || id != 2 {
		t.Fatalf("stream-1 -> %d, %v; want 2, true", id, ok)
	}
	if _, ok := client.StreamID("no-such"); ok {
		t.Fatal("unknown stream should not exist")
	}

	// 用 net.Pipe 模拟内网服务
	intraSrv, intraCli := net.Pipe()
	defer intraSrv.Close()
	var dialedStream string
	client.LocalDialer = func(streamId string) (net.Conn, error) {
		dialedStream = streamId
		return intraCli, nil
	}
	go func() { _ = client.Run() }()

	// 服务端请求在流1上建立连接(connID 77)
	req, _ := json.Marshal(ConnOpenReq{StreamID: 1, ConnID: 77})
	ts := <-tsCh
	if err := ts.sess.Send(&Message{Type: MsgConnOpen, StreamID: 1, Payload: req}); err != nil {
		t.Fatalf("send connOpen: %v", err)
	}

	select {
	case ack := <-ts.ConnAcks:
		if ack.ConnID != 77 || !ack.OK {
			t.Fatalf("connOpenAck = %+v; want connID 77 ok", ack)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting connOpenAck")
	}
	if dialedStream != "stream-0" {
		t.Fatalf("LocalDialer got streamId %q, want stream-0", dialedStream)
	}

	// 服务端 -> 内网服务(下行)
	if err := ts.sess.Send(&Message{Type: MsgData, StreamID: 77, Payload: []byte("down link")}); err != nil {
		t.Fatalf("send down link: %v", err)
	}
	if err := intraSrv.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := intraSrv.Read(buf)
	if err != nil || string(buf[:n]) != "down link" {
		t.Fatalf("intranet read = %q, %v; want down link", buf[:n], err)
	}

	// 内网服务 -> 服务端(上行)
	if err := intraSrv.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := intraSrv.Write([]byte("up link")); err != nil {
		t.Fatalf("intranet write: %v", err)
	}
	select {
	case msg := <-ts.Data:
		if msg.StreamID != 77 || string(msg.Payload) != "up link" {
			t.Fatalf("server got stream %d %q; want 77 up link", msg.StreamID, msg.Payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting up link frame")
	}

	// 保活:连续 ping 数次后连接仍然可用
	client.StartKeepAlive(10 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if _, err := intraSrv.Write([]byte("after keepalive")); err != nil {
		t.Fatalf("intranet write: %v", err)
	}
	select {
	case msg := <-ts.Data:
		if string(msg.Payload) != "after keepalive" {
			t.Fatalf("server got %q", msg.Payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting data after keepalive")
	}
}

func TestDialCtrlBadPassword(t *testing.T) {
	_, addr := startTestCtrlTCP(t, "right")

	_, err := DialCtrl("tcp", addr,
		HandshakeReq{ClientName: "dailer-x", KeyPassword: "wrong"},
		nil, 3*time.Second)
	if err == nil {
		t.Fatal("expected DialCtrl error for wrong password")
	}
}

func TestConnOpenRefusedWithoutDialer(t *testing.T) {
	tsCh, addr := startTestCtrlTCP(t, "pwd")

	client, err := DialCtrl("tcp", addr,
		HandshakeReq{KeyPassword: "pwd"},
		[]RegisterStreamReq{{ClientProxyName: "cp", StreamId: "s"}},
		3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl: %v", err)
	}
	defer client.Close()
	go func() { _ = client.Run() }()

	// 未设置 LocalDialer,开连接请求应被拒绝
	req, _ := json.Marshal(ConnOpenReq{StreamID: 1, ConnID: 9})
	ts := <-tsCh
	if err := ts.sess.Send(&Message{Type: MsgConnOpen, StreamID: 1, Payload: req}); err != nil {
		t.Fatalf("send connOpen: %v", err)
	}

	select {
	case ack := <-ts.ConnAcks:
		if ack.ConnID != 9 || ack.OK {
			t.Fatalf("connOpenAck = %+v; want connID 9 refused", ack)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting refused connOpenAck")
	}
}
