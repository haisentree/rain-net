package sniff

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSniffDispatch 单端口多协议:HTTP(按 Host 细分)、前缀魔数、默认兜底。
func TestSniffDispatch(t *testing.T) {
	var lc net.ListenConfig
	base, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	d, err := New(context.Background(), base, Config{
		HostRoutes:  map[string]string{"mgr.local": "mgr"},
		HTTPDefault: "http",
		Prefixes:    []PrefixRule{{Prefix: "524e", Target: "star"}}, // "RN" 魔数
		Default:     "raw",
	})
	if err != nil {
		t.Fatal(err)
	}

	mgrLn, _ := d.Listener("mgr")
	httpLn, _ := d.Listener("http")
	starLn, _ := d.Listener("star")
	rawLn, _ := d.Listener("raw")

	// 各目标服务
	go serveHTTP(mgrLn, "MGR-OK")
	go serveHTTP(httpLn, "HTTP-OK")
	go serveLines(starLn, "STAR-OK\n")
	go serveEcho(rawLn)

	if _, err := d.Listener("nope"); err == nil {
		t.Fatal("expected unknown target to fail")
	}

	// 1) HTTP + Host 命中 mgr 路由
	got := httpGet(t, base.Addr().String(), "mgr.local")
	if got != "MGR-OK" {
		t.Fatalf("mgr = %q", got)
	}
	// 2) HTTP 未命中 → http 默认
	got = httpGet(t, base.Addr().String(), "other.local")
	if got != "HTTP-OK" {
		t.Fatalf("http default = %q", got)
	}
	// 3) 前缀魔数 → star
	conn, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("RN-payload")); err != nil {
		t.Fatal(err)
	}
	// 半关闭表示请求结束(EF 反应式服务依赖此信号)
	if tc, ok := conn.(*net.TCPConn); ok {
		if err := tc.CloseWrite(); err != nil {
			t.Fatal(err)
		}
	}
	line := readLine(t, conn)
	if line != "STAR-OK" {
		t.Fatalf("star = %q", line)
	}
	conn.Close()
	// 4) 其余字节 → raw echo
	conn2, err := net.Dial("tcp", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn2.Write([]byte{0x16, 0x03, 0x01, 'x'}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn2, buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(buf), "\x16\x03\x01") {
		t.Fatalf("raw echo = %x", buf)
	}
	conn2.Close()
}

func serveLines(ln net.Listener, reply string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			// 读全对端数据(半关闭前),再应答——避免带未读数据关闭触发 RST
			_, _ = io.ReadAll(c)
			c.Write([]byte(reply))
			c.Close()
		}(c)
	}
}

func serveEcho(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			_, _ = io.Copy(c, c)
			c.Close()
		}(c)
	}
}

func serveHTTP(ln net.Listener, body string) {
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(body))
	})}
	_ = srv.Serve(ln)
}

func httpGet(t *testing.T, addr, host string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	req := "GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(conn)
	idx := strings.Index(string(data), "\r\n\r\n")
	if idx < 0 {
		t.Fatalf("no body in %q", data)
	}
	return strings.TrimSpace(string(data[idx+4:]))
}

func readLine(t *testing.T, c net.Conn) string {
	t.Helper()
	buf := make([]byte, 0, 32)
	one := make([]byte, 1)
	for {
		if _, err := c.Read(one); err != nil {
			t.Fatal(err)
		}
		if one[0] == '\n' {
			break
		}
		buf = append(buf, one[0])
		if len(buf) > 64 {
			t.Fatal("line too long")
		}
	}
	return string(buf)
}
