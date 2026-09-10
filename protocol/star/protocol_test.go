package star

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// testCtrlServer 模拟未来的 ctrlproxy 服务端:
// 握手 -> 循环处理消息(注册流分配ID/连接应答/Ping回Pong/数据投递到Data)
type testCtrlServer struct {
	sess      *Session
	streamSeq uint32
	Data      chan *Message
	ConnAcks  chan ConnOpenAck
}

func newTestCtrlServer(conn net.Conn) *testCtrlServer {
	return &testCtrlServer{
		sess:     NewSession(conn),
		Data:     make(chan *Message, 4096),
		ConnAcks: make(chan ConnOpenAck, 64),
	}
}

func (ts *testCtrlServer) serve(keyPassword string) error {
	if _, err := ServerHandshake(ts.sess, keyPassword); err != nil {
		return err
	}
	for {
		msg, err := ts.sess.Receive()
		if err != nil {
			return err
		}
		switch msg.Type {
		case MsgRegisterStream:
			if _, err := ServerRegisterStream(ts.sess, msg, func(r RegisterStreamReq) (uint32, error) {
				ts.streamSeq++
				return ts.streamSeq, nil
			}); err != nil {
				return err
			}
		case MsgConnOpenAck:
			var ack ConnOpenAck
			if err := DecodeControl(msg, &ack); err != nil {
				return err
			}
			ts.ConnAcks <- ack
		case MsgPing:
			if err := ts.sess.Send(&Message{Type: MsgPong, StreamID: msg.StreamID, Payload: msg.Payload}); err != nil {
				return err
			}
		case MsgData:
			ts.Data <- msg
		case MsgStreamClose:
			return nil
		}
	}
}

// dialTestClient 建立会话并完成握手,返回会话和注册流ID的辅助闭包
func dialTestClient(t *testing.T, conn net.Conn, password string) *Session {
	t.Helper()
	sess := NewSession(conn)
	ack, err := ClientHandshake(sess, HandshakeReq{
		ClientName:  "test-client",
		KeyPassword: password,
	})
	if err != nil {
		t.Fatalf("ClientHandshake: %v", err)
	}
	if !ack.OK {
		t.Fatalf("handshake rejected: %s", ack.Message)
	}
	return sess
}

func mustRegisterStream(t *testing.T, sess *Session, name, streamId string) uint32 {
	t.Helper()
	ack, err := ClientRegisterStream(sess, RegisterStreamReq{ClientProxyName: name, StreamId: streamId})
	if err != nil {
		t.Fatalf("ClientRegisterStream(%s): %v", streamId, err)
	}
	if !ack.OK {
		t.Fatalf("register stream %s rejected: %s", streamId, ack.Message)
	}
	if ack.StreamId != streamId {
		t.Fatalf("ack streamId = %q, want %q", ack.StreamId, streamId)
	}
	return ack.AssignID
}

func TestMessageRoundTrip(t *testing.T) {
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	in := &Message{Type: MsgData, StreamID: 42, Payload: payload}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	out, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if out.Type != in.Type || out.StreamID != in.StreamID || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", out, in)
	}

	// 空负载消息
	empty := &Message{Type: MsgPing, StreamID: CtrlStreamID}
	buf.Reset()
	if err := WriteMessage(&buf, empty); err != nil {
		t.Fatalf("WriteMessage empty: %v", err)
	}
	out, err = ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage empty: %v", err)
	}
	if out.Type != MsgPing || out.StreamID != CtrlStreamID || len(out.Payload) != 0 {
		t.Fatalf("empty message mismatch: %+v", out)
	}
}

func TestReadMessageRejectsBadVersion(t *testing.T) {
	head := make([]byte, HeaderSize)
	head[0] = ProtocolVersion + 1
	head[1] = MsgData

	_, err := ReadMessage(bytes.NewReader(head))
	if err == nil {
		t.Fatal("expected error for bad version, got nil")
	}
}

func TestReadMessageRejectsOversize(t *testing.T) {
	head := make([]byte, HeaderSize)
	head[0] = ProtocolVersion
	head[1] = MsgData
	binary.BigEndian.PutUint32(head[8:12], MaxPayloadSize+1)

	_, err := ReadMessage(bytes.NewReader(head))
	if err == nil {
		t.Fatal("expected error for oversize payload, got nil")
	}
}

func TestHandshakeOK(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ts := newTestCtrlServer(c2)
	go func() { _ = ts.serve("right-password") }()

	sess := dialTestClient(t, c1, "right-password")
	_ = sess
}

func TestHandshakeBadPassword(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ts := newTestCtrlServer(c2)
	serveErr := make(chan error, 1)
	go func() { serveErr <- ts.serve("right-password") }()

	sess := NewSession(c1)
	ack, err := ClientHandshake(sess, HandshakeReq{KeyPassword: "wrong-password"})
	if err != nil {
		t.Fatalf("ClientHandshake: %v", err)
	}
	if ack.OK {
		t.Fatal("expected ack.OK = false for wrong password")
	}
	if ack.Message != "invalid key password" {
		t.Fatalf("ack.Message = %q", ack.Message)
	}

	select {
	case err := <-serveErr:
		if err == nil {
			t.Fatal("expected server side handshake error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting server handshake result")
	}
}

func TestRegisterStreamFlow(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ts := newTestCtrlServer(c2)
	go func() { _ = ts.serve("pwd") }()

	sess := dialTestClient(t, c1, "pwd")

	id1 := mustRegisterStream(t, sess, "clientProxy-0", "stream-0")
	id2 := mustRegisterStream(t, sess, "clientProxy-1", "stream-1")

	if id1 != 1 || id2 != 2 {
		t.Fatalf("assigned ids = %d, %d; want 1, 2", id1, id2)
	}
}

func TestPingPong(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ts := newTestCtrlServer(c2)
	go func() { _ = ts.serve("pwd") }()

	sess := dialTestClient(t, c1, "pwd")

	rtt, err := sess.Ping(3 * time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	t.Logf("ping rtt: %v", rtt)
}

func TestConcurrentDataFrames(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ts := newTestCtrlServer(c2)
	serveErr := make(chan error, 1)
	go func() { serveErr <- ts.serve("pwd") }()

	sess := dialTestClient(t, c1, "pwd")
	streamID := mustRegisterStream(t, sess, "clientProxy-0", "stream-0")

	const goroutines = 8
	const perGoroutine = 50
	total := goroutines * perGoroutine

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			for i := 0; i < perGoroutine; i++ {
				payload := []byte{byte(g), byte(i)}
				if err := sess.Send(&Message{Type: MsgData, StreamID: streamID, Payload: payload}); err != nil {
					t.Errorf("send frame: %v", err)
					return
				}
			}
		}(g)
	}

	for i := 0; i < total; i++ {
		select {
		case msg := <-ts.Data:
			if msg.StreamID != streamID {
				t.Fatalf("frame %d streamID = %d, want %d", i, msg.StreamID, streamID)
			}
			if len(msg.Payload) != 2 {
				t.Fatalf("frame %d payload len = %d, want 2", i, len(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting frames, got %d of %d", i, total)
		}
	}
}

func TestFullFlowOverTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type serverResult struct {
		streams map[string]uint32
		err     error
	}
	dataCh := make(chan *Message, 16)
	resultCh := make(chan serverResult, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			resultCh <- serverResult{err: err}
			return
		}
		defer conn.Close()

		sess := NewSession(conn)
		streams := make(map[string]uint32)
		var seq uint32
		if _, err := ServerHandshake(sess, "key-password-1"); err != nil {
			resultCh <- serverResult{err: err}
			return
		}
		for {
			msg, err := sess.Receive()
			if err != nil {
				resultCh <- serverResult{streams: streams, err: err}
				return
			}
			switch msg.Type {
			case MsgRegisterStream:
				var req RegisterStreamReq
				if err := DecodeControl(msg, &req); err != nil {
					resultCh <- serverResult{err: err}
					return
				}
				seq++
				streams[req.StreamId] = seq
				ack := RegisterStreamAck{OK: true, StreamId: req.StreamId, AssignID: seq}
				if err := sess.SendControl(MsgRegisterStreamAck, ack); err != nil {
					resultCh <- serverResult{err: err}
					return
				}
			case MsgData:
				dataCh <- msg
			case MsgStreamClose:
				resultCh <- serverResult{streams: streams}
				return
			}
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	sess := dialTestClient(t, conn, "key-password-1")
	id := mustRegisterStream(t, sess, "clientProxy-0", "dailer-0-stream-1")

	if err := sess.Send(&Message{Type: MsgData, StreamID: id, Payload: []byte("hello over tcp")}); err != nil {
		t.Fatalf("send data: %v", err)
	}
	select {
	case msg := <-dataCh:
		if string(msg.Payload) != "hello over tcp" || msg.StreamID != id {
			t.Fatalf("received data = stream %d %q, want stream %d %q",
				msg.StreamID, msg.Payload, id, "hello over tcp")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting data frame")
	}

	if err := sess.Send(&Message{Type: MsgStreamClose, StreamID: id}); err != nil {
		t.Fatalf("send stream close: %v", err)
	}

	select {
	case res := <-resultCh:
		got, ok := res.streams["dailer-0-stream-1"]
		if !ok || got != 1 {
			t.Fatalf("server streams = %v, want dailer-0-stream-1 -> 1", res.streams)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting server result")
	}
}
