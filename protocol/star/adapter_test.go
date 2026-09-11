package star

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// adapterPair 建立经 net.Pipe 会话相连的两个 StreamHub:
// sideA 为拨号侧(OpenStream),sideB 为接受侧(读循环处理 ConnOpen 并应答)。
// 返回 A 的 hub 与 B 侧新流连接的通道。
func adapterPair(t *testing.T) (hubA, hubB *StreamHub, openedB chan *StreamConn) {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })

	hubA = NewStreamHub(NewSession(c1))
	hubB = NewStreamHub(NewSession(c2))
	openedB = make(chan *StreamConn, 1)

	// sideA 读循环:数据/关流/ack 都交给 hub
	go func() {
		sess := hubA.sess
		for {
			msg, err := sess.Receive()
			if err != nil {
				hubA.SessionClosed(err)
				return
			}
			hubA.Feed(msg)
		}
	}()

	// sideB 读循环:ConnOpen → 应答并建流;其余交 hub
	go func() {
		sess := hubB.sess
		for {
			msg, err := sess.Receive()
			if err != nil {
				hubB.SessionClosed(err)
				return
			}
			if msg.Type == MsgConnOpen {
				var req ConnOpenReq
				if err := DecodeControl(msg, &req); err != nil {
					continue
				}
				_ = sess.SendControl(MsgConnOpenAck, ConnOpenAck{ConnID: req.ConnID, OK: true})
				sc := hubB.NewStreamConn(req.ConnID)
				openedB <- sc
				continue
			}
			hubB.Feed(msg)
		}
	}()
	return hubA, hubB, openedB
}

func TestStreamConnBasic(t *testing.T) {
	hubA, _, openedB := adapterPair(t)

	scA, err := hubA.OpenStream(context.Background(), 1, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scB := <-openedB

	if _, err := scA.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(scB, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, []byte("ping")) {
		t.Fatalf("B got %q", buf)
	}
	if _, err := scB.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(scA, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, []byte("pong")) {
		t.Fatalf("A got %q", buf)
	}
}

// TestStreamConnHalfClose 半关闭契约:A 关写后 B 读到 EOF,
// B 仍可应答,A 能收到应答;B 也关写后 A 读到 EOF。
func TestStreamConnHalfClose(t *testing.T) {
	hubA, _, openedB := adapterPair(t)

	scA, err := hubA.OpenStream(context.Background(), 1, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scB := <-openedB

	go func() {
		if _, err := scA.Write([]byte("request")); err != nil {
			t.Error(err)
		}
		if err := scA.CloseWrite(); err != nil {
			t.Error(err)
		}
	}()

	buf := make([]byte, 7) // "request" 恰 7 字节
	if _, err := io.ReadFull(scB, buf); err != nil {
		t.Fatal(err)
	}
	// A 已半关闭:B 读到 EOF(核心契约),但写方向仍可用
	if _, err := scB.Read(buf); err != io.EOF {
		t.Fatalf("B read after half close = %v, want EOF", err)
	}
	if _, err := scB.Write([]byte("reply")); err != nil {
		t.Fatal(err)
	}
	if err := scB.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(scA) // A 读到应答 + EOF
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "reply" {
		t.Fatalf("A got %q", got)
	}
}

// TestStreamConnFullClose 全关帧终止整条连接:B Close 后 A 读以 EOF 结束。
func TestStreamConnFullClose(t *testing.T) {
	hubA, _, openedB := adapterPair(t)

	scA, err := hubA.OpenStream(context.Background(), 1, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scB := <-openedB

	if _, err := scA.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := scB.Close(); err != nil {
		t.Fatal(err)
	}
	// 全关:A 的读以 EOF 结束
	if _, err := scA.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("A read = %v, want EOF", err)
	}
}

// TestStreamHubSessionClosed 会话断开:所有流连接以 EOF 解除阻塞。
func TestStreamHubSessionClosed(t *testing.T) {
	hubA, _, openedB := adapterPair(t)

	scA, err := hubA.OpenStream(context.Background(), 1, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	<-openedB

	// 断开会话底层连接(hubB 的读循环随即 SessionClosed;A 侧同样)
	hubA.sess.Conn().Close()
	time.Sleep(50 * time.Millisecond)

	if _, err := scA.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("A read after session close = %v, want EOF", err)
	}
	if _, err := hubA.OpenStream(context.Background(), 2, 100*time.Millisecond); err == nil {
		t.Fatal("expected OpenStream to fail after session closed")
	}
}

// TestStreamConnLargeWrite 大于单帧上限的写入自动分帧,对端按序收全。
func TestStreamConnLargeWrite(t *testing.T) {
	hubA, _, openedB := adapterPair(t)

	scA, err := hubA.OpenStream(context.Background(), 1, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scB := <-openedB

	payload := bytes.Repeat([]byte{0xAB}, 200<<10) // 200KB,跨 4 帧
	go func() {
		if _, err := scA.Write(payload); err != nil {
			t.Error(err)
		}
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(scB, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("large payload mismatch")
	}
}
