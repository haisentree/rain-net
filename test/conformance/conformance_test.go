// Package conformance 是传输一致性套件(docs/design.md §九.1):
// 同一组场景参数化地跑在每一个 Transport 实现上,作为半关闭等契约的机器保障。
// 新增传输的验收标准 = 通过本套件全部场景。
package conformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"hash/crc32"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/haisentree/rain-net/internal/transport"
	"github.com/haisentree/rain-net/pkg/rainnet"
)

const dialTimeout = 5 * time.Second

// Caps 传输能力声明:场景按能力断言(半关闭矩阵的机器保障)。
type Caps struct {
	HalfClose bool // 支持 CloseWrite(不支持者走降级场景)
}

// runSuite 对一个 Transport 实现跑全部一致性场景(默认支持半关闭)。
func runSuite(t *testing.T, name string, tr transport.Transport) {
	runSuiteCaps(t, name, tr, Caps{HalfClose: true})
}

func runSuiteCaps(t *testing.T, name string, tr transport.Transport, caps Caps) {
	t.Run(name+"/echo", func(t *testing.T) { scenarioEcho(t, tr) })
	t.Run(name+"/largeTransfer", func(t *testing.T) { scenarioLargeTransfer(t, tr) })
	if caps.HalfClose {
		t.Run(name+"/halfClose", func(t *testing.T) { scenarioHalfClose(t, tr) })
	} else {
		t.Run(name+"/halfCloseDegraded", func(t *testing.T) { scenarioHalfCloseDegraded(t, tr) })
	}
	t.Run(name+"/concurrent", func(t *testing.T) { scenarioConcurrent(t, tr) })
	t.Run(name+"/deadline", func(t *testing.T) { scenarioDeadline(t, tr) })
	t.Run(name+"/contextCancel", func(t *testing.T) { scenarioContextCancel(t, tr) })
}

// scenarioHalfCloseDegraded 无半关闭能力的传输(如 ws)降级语义:
// 客户端写完整条关闭;服务端必须完整收到请求(帧序与完整性不因降级丢失),
// 客户端随后读到关闭。
func scenarioHalfCloseDegraded(t *testing.T, tr transport.Transport) {
	type received struct {
		mu   sync.Mutex
		data []byte
	}
	rec := &received{}
	addr := serve(t, tr, func(c net.Conn) {
		buf := make([]byte, 7)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		rec.mu.Lock()
		rec.data = append(rec.data, buf...)
		rec.mu.Unlock()
		c.Close()
	})

	c := rainnet.Wrap(dial(t, tr, addr), rainnet.Meta{})
	if _, err := c.Write([]byte("request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 本地关闭后 Read 返回 EOF 或 net.ErrClosed(含包装)均为合法(Go 惯例)
	if _, err := c.Read(make([]byte, 1)); err != io.EOF && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("read after close = %v, want EOF or ErrClosed", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rec.mu.Lock()
		got := string(rec.data)
		rec.mu.Unlock()
		if got == "request" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server received %q, want %q", got, "request")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// serve 启动一个基于 tr 的测试服务,每个连接交给 handler 处理,返回服务地址。
func serve(t *testing.T, tr transport.Transport, handler func(c net.Conn)) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := tr.Listen(ctx, "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { cancel(); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handler(c)
		}
	}()
	return ln.Addr().String()
}

func dial(t *testing.T, tr transport.Transport, addr string) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	t.Cleanup(cancel)
	c, err := tr.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// scenarioEcho 基本回环:写入的字节原样返回。
func scenarioEcho(t *testing.T, tr transport.Transport) {
	addr := serve(t, tr, func(c net.Conn) {
		_, _ = io.Copy(c, c)
		c.Close()
	})

	c := dial(t, tr, addr)
	payload := []byte("ping through transport")
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("echo mismatch: %q", buf)
	}
}

// scenarioLargeTransfer 大流量:4MB 随机数据回环,校验和必须一致。
func scenarioLargeTransfer(t *testing.T, tr transport.Transport) {
	addr := serve(t, tr, func(c net.Conn) {
		_, _ = io.Copy(c, c)
		c.Close()
	})

	c := dial(t, tr, addr)
	payload := make([]byte, 4<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	wantSum := crc32.ChecksumIEEE(payload)

	done := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		done <- err
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("read echoed payload: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("write: %v", err)
	}
	if gotSum := crc32.ChecksumIEEE(got); gotSum != wantSum {
		t.Fatalf("checksum mismatch: got %08x want %08x", gotSum, wantSum)
	}
}

// scenarioHalfClose 半关闭契约(核心场景):
// 客户端 CloseWrite 后,服务端读到 EOF、应答并关闭;
// 客户端必须能完整收到应答,且之后读到 EOF。
func scenarioHalfClose(t *testing.T, tr transport.Transport) {
	const reply = "half-close-ok"
	addr := serve(t, tr, func(c net.Conn) {
		rc := rainnet.Wrap(c, rainnet.Meta{Listener: "conformance"})
		// 读到 EOF(对端半关闭),再应答
		req, err := io.ReadAll(rc)
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if len(req) == 0 {
			t.Error("server got empty request")
			return
		}
		if _, err := rc.Write([]byte(reply)); err != nil {
			t.Errorf("server write: %v", err)
			return
		}
		_ = rc.CloseWrite() // 服务端也半关闭,随后关闭
		_ = rc.Close()
	})

	c := rainnet.Wrap(dial(t, tr, addr), rainnet.Meta{Listener: "conformance-client"})
	if _, err := c.Write([]byte("request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := c.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("read reply after half-close: %v", err)
	}
	if string(got) != reply {
		t.Fatalf("reply = %q, want %q", got, reply)
	}
}

// scenarioConcurrent 并发流:多个连接互不串扰。
func scenarioConcurrent(t *testing.T, tr transport.Transport) {
	addr := serve(t, tr, func(c net.Conn) {
		_, _ = io.Copy(c, c)
		c.Close()
	})

	const n = 8
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			payload := bytes.Repeat([]byte{byte('a' + i)}, 64<<10)
			c, err := func() (net.Conn, error) {
				ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
				defer cancel()
				return tr.Dial(ctx, addr)
			}()
			if err != nil {
				results <- err
				return
			}
			defer c.Close()
			if _, err := c.Write(payload); err != nil {
				results <- err
				return
			}
			buf := make([]byte, len(payload))
			if _, err := io.ReadFull(c, buf); err != nil {
				results <- err
				return
			}
			if !bytes.Equal(buf, payload) {
				results <- os.ErrInvalid
				return
			}
			results <- nil
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent stream %d failed: %v", i, err)
		}
	}
}

// scenarioDeadline 读截止时间必须生效。
func scenarioDeadline(t *testing.T, tr transport.Transport) {
	addr := serve(t, tr, func(c net.Conn) {
		_, _ = io.Copy(io.Discard, c)
	})

	c := dial(t, tr, addr)
	if err := c.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected read to time out")
	} else if !os.IsTimeout(err) && !isNetTimeout(err) {
		t.Fatalf("read error not timeout: %v", err)
	}
}

func isNetTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

// scenarioContextCancel ctx 取消:Dial 失败;Listen 关闭后 Accept 报错。
func scenarioContextCancel(t *testing.T, tr transport.Transport) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tr.Dial(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("expected dial with cancelled ctx to fail")
	}

	lctx, lcancel := context.WithCancel(context.Background())
	ln, err := tr.Listen(lctx, "127.0.0.1:0")
	if err != nil {
		lcancel()
		t.Fatalf("listen: %v", err)
	}
	lcancel()
	acceptErr := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		acceptErr <- err
	}()
	select {
	case err := <-acceptErr:
		if err == nil {
			t.Fatal("expected Accept error after ctx cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener not closed after ctx cancel")
	}
}
