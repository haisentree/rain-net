package ws

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startEchoServer 服务端 Accept 后把收到的消息原样发回
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				wsConn, err := Accept(c)
				if err != nil {
					c.Close()
					return
				}
				defer wsConn.Close()
				for {
					msg, err := wsConn.ReadMessage()
					if err != nil {
						return
					}
					if err := wsConn.WriteMessage(msg); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestRoundTrip(t *testing.T) {
	addr := startEchoServer(t)

	client, err := Dial(addr, 3*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	messages := [][]byte{
		[]byte("hello"),
		bytes.Repeat([]byte{0xAB}, 300*1024), // 走 64 位长度编码路径
		[]byte("after large"),
	}
	for _, msg := range messages {
		if err := client.WriteMessage(msg); err != nil {
			t.Fatalf("write %d bytes: %v", len(msg), err)
		}
	}
	for i, want := range messages {
		client.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		got, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("message %d: got %d bytes, want %d bytes (equal=%v)", i, len(got), len(want), bytes.Equal(got, want))
		}
	}
}

// handshakePipe 在 net.Pipe 上完成一次真实握手,返回(客户端,服务端)连接
// net.Pipe 同步无缓冲:握手请求/应答必须两端配合收发
func handshakePipe(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	c1, c2 := net.Pipe()

	type res struct {
		c   *Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := Accept(c2)
		ch <- res{c, err}
	}()

	req := "GET / HTTP/1.1\r\nHost: t\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := c1.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	buf := make([]byte, 256)
	_ = c1.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := c1.Read(buf)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "101 Switching Protocols") {
		t.Fatalf("handshake response = %q", buf[:n])
	}
	_ = c1.SetReadDeadline(time.Time{})

	srv := <-ch
	if srv.err != nil {
		t.Fatalf("Accept: %v", srv.err)
	}
	client := &Conn{conn: c1, br: bufio.NewReader(c1), client: true}
	return client, srv.c
}

func TestFragmentation(t *testing.T) {
	client, server := handshakePipe(t)

	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	// net.Pipe 是同步的,必须先起读端再写
	go func() {
		msg, err := server.ReadMessage()
		ch <- result{msg, err}
	}()
	time.Sleep(50 * time.Millisecond)

	if err := client.writeFrame(false, OpText, []byte("hello ")); err != nil {
		t.Fatalf("write frag1: %v", err)
	}
	if err := client.writeFrame(true, OpContinuation, []byte("world")); err != nil {
		t.Fatalf("write frag2: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("ReadMessage: %v", r.err)
		}
		if string(r.msg) != "hello world" {
			t.Fatalf("assembled = %q", r.msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting assembled message")
	}
}

func TestPingAutoPong(t *testing.T) {
	client, server := handshakePipe(t)

	// 服务端读循环自动回 pong;先起读端避免 pipe 写阻塞
	go func() { _, _ = server.ReadMessage() }()
	time.Sleep(50 * time.Millisecond)

	if err := client.writeControl(OpPing, []byte("hb")); err != nil {
		t.Fatalf("ping: %v", err)
	}

	client.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, op, payload, err := client.readFrame()
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if op != OpPong || string(payload) != "hb" {
		t.Fatalf("pong = op:%02x payload:%q", op, payload)
	}
}

func TestCloseSemantics(t *testing.T) {
	client, server := handshakePipe(t)

	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := server.ReadMessage()
		ch <- result{msg, err}
	}()
	time.Sleep(50 * time.Millisecond)

	// 客户端发起关闭
	go func() {
		_ = client.writeControl(OpClose, nil)
	}()

	// 客户端读走服务端回的关闭帧(不读会让服务端的应答写在 pipe 上永久阻塞)
	client.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, op, _, err := client.readFrame()
	if err != nil {
		t.Fatalf("read close reply: %v", err)
	}
	if op != OpClose {
		t.Fatalf("reply opcode = %02x, want close", op)
	}

	select {
	case r := <-ch:
		if r.err != io.EOF {
			t.Fatalf("server read = %v, want io.EOF", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting server EOF")
	}
}

func TestAcceptRejectsPlainHTTP(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		req := "GET / HTTP/1.1\r\nHost: x\r\n\r\n"
		c1.Write([]byte(req))
		io.Copy(io.Discard, c1)
	}()

	_, err := Accept(c2)
	if err == nil {
		t.Fatal("expected Accept to reject plain HTTP request")
	}
}

func TestAcceptRejectsMissingKey(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Write(c1)
		io.Copy(io.Discard, c1)
	}()

	if _, err := Accept(c2); err == nil {
		t.Fatal("expected Accept to reject missing Sec-WebSocket-Key")
	}
}
