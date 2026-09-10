package starserver

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"rain-net/protocol/star"
)

// startEchoServer 起一个本地 echo 服务,模拟内网中的服务
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// startFixedServer 起一个本地服务:读掉请求后回复固定内容,用于区分后端
func startFixedServer(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fixed listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 512)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
					if _, err := c.Write([]byte(reply)); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// proxyRequest 模拟一次外部请求,返回后端应答
func proxyRequest(t *testing.T, addr string) string {
	t.Helper()
	ext, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer ext.Close()
	if err := ext.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ext.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 64)
	n, err := ext.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

// TestProxyFailoverAcrossClients 多客户端并存与故障转移:
// 两个客户端各注册一条流,共同承接外部流量;一个下线后流量全部切换到另一个
func TestProxyFailoverAcrossClients(t *testing.T) {
	backend0 := startFixedServer(t, "from-0")
	backend1 := startFixedServer(t, "from-1")

	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings: Settings{
			ClientProxy: []ClientProxy{
				{ClientProxyName: "clientProxy-0", KeyPassword: "pwd-0"},
				{ClientProxyName: "clientProxy-1", KeyPassword: "pwd-1"},
			},
		},
	}, hub)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ctrl listen: %v", err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

	connectTable := NewConnectTable()
	connectTable.Seed([]ConnectItem{
		{ProxyName: "p0", ClientProxyName: "clientProxy-0", StreamId: "stream-0"},
		{ProxyName: "p0", ClientProxyName: "clientProxy-1", StreamId: "stream-0"},
	})
	proxy, perr := NewProxyServer("svc", &ListenerList{
		Type:      "proxy",
		Transport: "tcp",
		Settings:  Settings{ProxyName: "p0"},
	}, connectTable, hub)
	if perr != nil {
		t.Fatalf("NewProxyServer: %v", perr)
	}
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer proxyLn.Close()
	go func() { _ = proxy.Serve(proxyLn) }()

	connect := func(name, password, clientProxyName, backend string) *star.CtrlClient {
		t.Helper()
		client, err := star.DialCtrl("tcp", ctrlLn.Addr().String(),
			star.HandshakeReq{ClientName: name, KeyPassword: password},
			[]star.RegisterStreamReq{{ClientProxyName: clientProxyName, StreamId: "stream-0"}},
			3*time.Second)
		if err != nil {
			t.Fatalf("DialCtrl %s: %v", name, err)
		}
		client.LocalDialer = func(streamId string) (net.Conn, error) {
			return net.Dial("tcp", backend)
		}
		go func() { _ = client.Run() }()
		return client
	}

	client0 := connect("dailer-0", "pwd-0", "clientProxy-0", backend0)
	client1 := connect("dailer-1", "pwd-1", "clientProxy-1", backend1)

	// 两个客户端都在:round-robin 轮流承接
	waitFor(t, 3*time.Second, "both streams registered", func() bool {
		return hub.Streams.Len() == 2
	})
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		seen[proxyRequest(t, proxyLn.Addr().String())] = true
	}
	if !seen["from-0"] || !seen["from-1"] {
		t.Fatalf("expected both backends, got %v", seen)
	}

	// client-0 下线:流量全部切换到 client-1
	client0.Close()
	waitFor(t, 5*time.Second, "failover ready", func() bool {
		return hub.Streams.Len() == 1
	})
	for i := 0; i < 5; i++ {
		if got := proxyRequest(t, proxyLn.Addr().String()); got != "from-1" {
			t.Fatalf("after failover got %q, want from-1", got)
		}
	}

	// client-0 恢复上线:重新参与轮转
	client0 = connect("dailer-0", "pwd-0", "clientProxy-0", backend0)
	defer client0.Close()
	defer client1.Close()
	waitFor(t, 5*time.Second, "recovery ready", func() bool {
		return hub.Streams.Len() == 2
	})
	seen = map[string]bool{}
	for i := 0; i < 10; i++ {
		seen[proxyRequest(t, proxyLn.Addr().String())] = true
	}
	if !seen["from-0"] || !seen["from-1"] {
		t.Fatalf("after recovery expected both backends, got %v", seen)
	}
}
// startCloseAfterReplyServer 模拟 HTTP/1.0 风格的内网服务:
// 读到请求后立即回复并关闭连接(如 python http.server)
func startCloseAfterReplyServer(t *testing.T, reply string) string {
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
				defer c.Close()
				buf := make([]byte, 512)
				if _, err := c.Read(buf); err != nil {
					return
				}
				_, _ = c.Write([]byte(reply))
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// TestProxyBackendClosesAfterReply 回归测试:后端响应后立即关闭连接时,
// 在途下行数据必须送达外部用户(不能被关流帧提前丢弃)
func TestProxyBackendClosesAfterReply(t *testing.T) {
	backend := startCloseAfterReplyServer(t, "HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nok")

	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings: Settings{
			ClientProxy: []ClientProxy{{ClientProxyName: "clientProxy-0", KeyPassword: "pwd"}},
		},
	}, hub)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ctrl listen: %v", err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

	connectTable := NewConnectTable()
	connectTable.Seed([]ConnectItem{{ProxyName: "p0", ClientProxyName: "clientProxy-0", StreamId: "stream-0"}})
	proxy, perr := NewProxyServer("svc", &ListenerList{
		Type:      "proxy",
		Transport: "tcp",
		Settings:  Settings{ProxyName: "p0"},
	}, connectTable, hub)
	if perr != nil {
		t.Fatalf("NewProxyServer: %v", perr)
	}
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer proxyLn.Close()
	go func() { _ = proxy.Serve(proxyLn) }()

	client, err := star.DialCtrl("tcp", ctrlLn.Addr().String(),
		star.HandshakeReq{ClientName: "dailer-0", KeyPassword: "pwd"},
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}},
		3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl: %v", err)
	}
	defer client.Close()
	client.LocalDialer = func(streamId string) (net.Conn, error) {
		return net.Dial("tcp", backend)
	}
	go func() { _ = client.Run() }()

	want := "HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nok"
	for i := 0; i < 5; i++ {
		start := time.Now()
		got := proxyRequest(t, proxyLn.Addr().String())
		if got != want {
			t.Fatalf("request %d: got %q, want %q", i, got, want)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("request %d took %v, in-flight data likely dropped", i, elapsed)
		}
	}
}

// TestReverseProxyEndToEnd 完整数据面:
// 外部连接 -> ProxyServer -> ctrl 通道 -> CtrlClient -> 内网 echo 服务
func TestReverseProxyEndToEnd(t *testing.T) {
	echoAddr := startEchoServer(t)

	hub := NewCtrlHub()

	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings:  Settings{KeyPassword: "pwd"},
	}, hub)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ctrl listen: %v", err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

	connectTable := NewConnectTable()
	connectTable.Seed([]ConnectItem{{
		ProxyName:       "listener-proxy-0",
		ClientProxyName: "clientProxy-0",
		StreamId:        "stream-0",
	}})
	proxy, perr := NewProxyServer("svc", &ListenerList{
		Type:      "proxy",
		Transport: "tcp",
		Settings:  Settings{ProxyName: "listener-proxy-0"},
	}, connectTable, hub)
	if perr != nil {
		t.Fatalf("NewProxyServer: %v", perr)
	}
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer proxyLn.Close()
	go func() { _ = proxy.Serve(proxyLn) }()

	// 客户端接入 ctrl 通道,内网拨号指向 echo 服务
	client, err := star.DialCtrl("tcp", ctrlLn.Addr().String(),
		star.HandshakeReq{ClientName: "dailer-0", KeyPassword: "pwd"},
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-0"}},
		3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl: %v", err)
	}
	defer client.Close()

	// LocalDialer 在 Run 期间被读,目标地址用原子变量承载以模拟切换
	var backend atomic.Value
	backend.Store(echoAddr)
	client.LocalDialer = func(streamId string) (net.Conn, error) {
		return net.Dial("tcp", backend.Load().(string))
	}
	go func() { _ = client.Run() }()

	// 等流注册进 hub(注册应答先行,这里大概率立即就绪)
	waitFor(t, 3*time.Second, "stream registered", func() bool {
		_, ok := hub.Streams.GetByName("clientProxy-0", "stream-0")
		return ok
	})

	// 多条并发外部连接,每条独立打洞
	const conns = 4
	results := make(chan error, conns)
	for i := 0; i < conns; i++ {
		go func(i int) {
			payload := []byte("hello through tunnel-")
			payload = append(payload, byte('0'+i))

			ext, err := net.Dial("tcp", proxyLn.Addr().String())
			if err != nil {
				results <- err
				return
			}
			defer ext.Close()

			if err := ext.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				results <- err
				return
			}
			if _, err := ext.Write(payload); err != nil {
				results <- err
				return
			}
			buf := make([]byte, len(payload))
			if _, err := io.ReadFull(ext, buf); err != nil {
				results <- err
				return
			}
			if string(buf) != string(payload) {
				t.Errorf("conn %d echo mismatch: %q", i, buf)
				results <- err
				return
			}
			results <- nil
		}(i)
	}
	for i := 0; i < conns; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("tunnel conn failed: %v", err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("timeout waiting tunnel conn")
		}
	}

	// 内网服务不可达时,外部连接应快速失败(开连接被拒绝)
	backend.Store("intranet-down:0")
	ext, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer ext.Close()
	_ = ext.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := ext.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := ext.Read(buf); err == nil {
		t.Fatal("expected conn close when intranet unreachable")
	}

	// 客户端断开后,外部连接应无法再建立
	client.Close()
	waitFor(t, 3*time.Second, "streams cleanup", func() bool {
		return hub.Streams.Len() == 0
	})
	ext2, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer ext2.Close()
	_ = ext2.SetDeadline(time.Now().Add(3 * time.Second))
	// 没有可用流,服务端应直接关闭连接
	if _, err := ext2.Write([]byte("y")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ext2.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected conn close when no stream available")
	}
}
