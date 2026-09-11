package conformance

import (
	"context"
	"crypto/rand"
	"github.com/haisentree/rain-net/internal/transport"
	"io"
	"testing"
	"time"
)

// benchEcho 吞吐基准:1MB 载荷经传输回环,单连接串行写读。
func benchEcho(b *testing.B, tr transport.Transport, size int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := tr.Listen(ctx, "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()

	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		b.Fatal(err)
	}
	got := make([]byte, size)

	dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dcancel()
	conn, err := tr.Dial(dctx, ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
		if _, err := io.ReadFull(conn, got); err != nil {
			b.Fatal(err)
		}
	}
}
