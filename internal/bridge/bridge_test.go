package bridge

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

// setup 建立拓扑:client ↔ [src | Bridge | dst] ↔ peer
// Bridge 桥接 src↔dst(单一读者,不可共享);client/peer 交给测试驱动业务。
func setup(t *testing.T) (client, src, dst, peer net.Conn) {
	t.Helper()
	cLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cLn.Close() })
	bLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bLn.Close() })

	type accepted struct {
		c   net.Conn
		err error
	}
	srcCh := make(chan accepted, 1)
	go func() {
		c, err := cLn.Accept()
		srcCh <- accepted{c, err}
	}()
	peerCh := make(chan accepted, 1)
	go func() {
		c, err := bLn.Accept()
		peerCh <- accepted{c, err}
	}()

	client, err = net.Dial("tcp", cLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	dst, err = net.Dial("tcp", bLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dst.Close() })

	sa := <-srcCh
	if sa.err != nil {
		t.Fatal(sa.err)
	}
	t.Cleanup(func() { sa.c.Close() })
	p := <-peerCh
	if p.err != nil {
		t.Fatal(p.err)
	}
	peer = p.c
	t.Cleanup(func() { peer.Close() })
	return client, sa.c, dst, peer
}

func runBridge(t *testing.T, ctx context.Context, src, dst net.Conn, opts ...Option) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(ctx,
			rainnet.Wrap(src, rainnet.Meta{}),
			rainnet.Wrap(dst, rainnet.Meta{}),
			opts...)
	}()
	return done
}

// TestBridgeHalfClose 核心契约:客户端关写后,后端读到 EOF 才应答(HTTP/1.0 风格),
// 应答必须完整穿过桥送达客户端,之后客户端才读到 EOF。
func TestBridgeHalfClose(t *testing.T) {
	client, src, dst, peer := setup(t)

	const reply = "HTTP/1.0 200 OK\r\n\r\nok"
	go func() {
		p := rainnet.Wrap(peer, rainnet.Meta{})
		req, err := io.ReadAll(p)
		if err != nil || len(req) == 0 {
			t.Errorf("peer read: %v (%d bytes)", err, len(req))
			return
		}
		p.Write([]byte(reply))
		p.CloseWrite()
		p.Close()
	}()

	done := runBridge(t, context.Background(), src, dst)

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := rainnet.Wrap(client, rainnet.Meta{}).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != reply {
		t.Fatalf("reply = %q, want %q", got, reply)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return")
	}
}

// TestBridgeDrainTimeout 半关闭后对端挂死:排空超时必须触发,双端释放。
func TestBridgeDrainTimeout(t *testing.T) {
	client, src, dst, peer := setup(t)

	done := runBridge(t, context.Background(), src, dst, WithDrainTimeout(200*time.Millisecond))

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := rainnet.Wrap(client, rainnet.Meta{}).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	// 后端收到请求后装死:不应答、不关闭
	buf := make([]byte, 7)
	if _, err := io.ReadFull(peer, buf); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected drain timeout error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return on drain timeout")
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected client conn closed after drain timeout")
	}
}

// TestBridgeErrorPropagation 一端出错:立即双关,错误上抛,另一端连接被释放。
func TestBridgeErrorPropagation(t *testing.T) {
	client, src, dst, peer := setup(t)

	go func() {
		// SO_LINGER=0 使 Close 发 RST 而非 FIN:dst 侧读写立即报错(干净 FIN 只会得到 EOF)
		if tc, ok := peer.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		peer.Close()
	}()

	done := runBridge(t, context.Background(), src, dst)

	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected bridge error when backend dies")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return")
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected client conn closed after backend error")
	}
}

// TestBridgeContextCancel ctx 取消:Bridge 返回 ctx.Err(),双端在有限时间内释放。
func TestBridgeContextCancel(t *testing.T) {
	client, src, dst, _ := setup(t) // peer 由 setup 的 cleanup 持有,保持连接打开即可

	ctx, cancel := context.WithCancel(context.Background())
	done := runBridge(t, ctx, src, dst)

	time.Sleep(50 * time.Millisecond) // 让桥先跑起来
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return after cancel")
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected client conn closed after cancel")
	}
}

// TestBridgeDegradedHalfClose 不支持 CloseWrite 的连接(net.Pipe):
// 半关闭降级为整条关闭,桥正常收尾、数据完整送达。
func TestBridgeDegradedHalfClose(t *testing.T) {
	pc, ps := net.Pipe() // client 侧:src=pc,测试驱动 ps
	bc, bs := net.Pipe() // backend 侧:dst=bc,peer=bs
	t.Cleanup(func() { pc.Close(); ps.Close(); bc.Close(); bs.Close() })

	// 后端:读请求 → 应答 → 关闭(全程无半关闭能力)
	go func() {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(bs, buf); err != nil {
			t.Errorf("backend read: %v", err)
			return
		}
		bs.Write([]byte("pong"))
		bs.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(),
			rainnet.Wrap(pc, rainnet.Meta{}),
			rainnet.Wrap(bc, rainnet.Meta{}))
	}()

	if _, err := ps.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(ps, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != "pong" {
		t.Fatalf("reply = %q", got)
	}
	ps.Close() // 客户端收完即关:第二方向 EOF,桥得以收尾(降级语义)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return")
	}
}
