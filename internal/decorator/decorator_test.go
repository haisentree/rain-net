package decorator

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

func testPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	cLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cLn.Close() })

	type accepted struct {
		c   net.Conn
		err error
	}
	ch := make(chan accepted, 1)
	go func() {
		c, err := cLn.Accept()
		ch <- accepted{c, err}
	}()

	client, err = net.Dial("tcp", cLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	srv := <-ch
	if srv.err != nil {
		t.Fatal(srv.err)
	}
	t.Cleanup(func() { srv.c.Close() })
	return client, srv.c
}

func TestAccounting(t *testing.T) {
	client, server := testPair(t)

	wrapped, ct := NewAccounting(rainnet.Wrap(client, rainnet.Meta{}))
	if _, err := wrapped.Write([]byte("hello accounting")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(wrapped, buf); err != nil {
		t.Fatal(err)
	}
	if got := ct.Out.Load(); got != 16 {
		t.Fatalf("Out = %d, want 16", got)
	}
	if got := ct.In.Load(); got != 16 {
		t.Fatalf("In = %d, want 16", got)
	}
	// CloseWrite 透传(TCP 底层支持)
	if err := wrapped.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite passthrough: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	client, server := testPair(t)
	go io.Copy(io.Discard, server)

	limited := NewRateLimit(rainnet.Wrap(client, rainnet.Meta{}), 1000) // 1000 B/s,burst 1000
	payload := make([]byte, 1500)

	start := time.Now()
	if _, err := limited.Write(payload); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// burst 1000 字节立发,余 500 字节需 ~0.5s
	if elapsed < 400*time.Millisecond {
		t.Fatalf("write returned too fast (%v), rate limit not effective", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("write too slow (%v)", elapsed)
	}
}

func TestGateAdmission(t *testing.T) {
	g := NewGate(2, 0)

	if err := g.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Acquire("u2"); err != nil {
		t.Fatal(err)
	}
	err := g.Acquire("u3")
	if rainnet.KindOf(err) != rainnet.KindPolicy {
		t.Fatalf("third acquire = %v, want policy error", err)
	}
	g.Release()
	if err := g.Acquire("u3"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if g.InUse() != 2 {
		t.Fatalf("InUse = %d, want 2", g.InUse())
	}
}

func TestGateIdentityQPS(t *testing.T) {
	g := NewGate(0, 2) // 每身份 2/s

	for i := 0; i < 2; i++ {
		if err := g.Acquire("alice"); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	if err := g.Acquire("alice"); rainnet.KindOf(err) != rainnet.KindPolicy {
		t.Fatalf("third acquire = %v, want policy error", err)
	}
	// 其他身份不受影响
	if err := g.Acquire("bob"); err != nil {
		t.Fatalf("bob acquire: %v", err)
	}
	// 空 identity 不受限速约束
	if err := g.Acquire(""); err != nil {
		t.Fatalf("empty identity acquire: %v", err)
	}
}
