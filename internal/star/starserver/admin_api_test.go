package starserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"rain-net/protocol/star"
)

func TestConnectTable(t *testing.T) {
	ct := NewConnectTable()

	ct.Seed([]ConnectItem{
		{ProxyName: "p0", ClientProxyName: "cp-0", StreamId: "s-0"},
		{ProxyName: "p0", ClientProxyName: "cp-1", StreamId: "s-1"},
	})
	if got := len(ct.For("p0")); got != 2 {
		t.Fatalf("For(p0) len = %d, want 2", got)
	}

	// 重复添加被拒绝
	if ct.Add(ConnectItem{ProxyName: "p0", ClientProxyName: "cp-0", StreamId: "s-0"}) {
		t.Fatal("expected duplicate add to fail")
	}
	if !ct.Add(ConnectItem{ProxyName: "p0", ClientProxyName: "cp-2", StreamId: "s-2"}) {
		t.Fatal("expected new add to succeed")
	}

	if !ct.Remove("p0", "cp-1", "s-1") {
		t.Fatal("expected remove to succeed")
	}
	if ct.Remove("p0", "cp-1", "s-1") {
		t.Fatal("expected second remove to fail")
	}
	if got := len(ct.For("p0")); got != 2 {
		t.Fatalf("For(p0) len after remove = %d, want 2", got)
	}

	// For 返回副本,外部修改不影响内部状态
	rules := ct.For("p0")
	rules[0] = ConnectItem{}
	if got := len(ct.For("p0")); got != 2 {
		t.Fatalf("For(p0) len after external mutation = %d, want 2", got)
	}
}

func TestAdminAPI(t *testing.T) {
	backend := startCloseAfterReplyServer(t, "HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nok")

	hub := NewCtrlHub()
	connectTable := NewConnectTable()

	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings:  Settings{}, // 初始无任何凭据,全部经 admin API 动态发放
	}, hub)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ctrl listen: %v", err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

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

	admin := NewAdminServer("admin", &ListenerList{
		Addr:     "127.0.0.1:0",
		Settings: Settings{AdminPassword: "secret"},
	}, hub, connectTable, []*CtrlProxyServer{ctrl})
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("admin listen: %v", err)
	}
	defer adminLn.Close()
	go func() { _ = admin.Serve(adminLn) }()

	base := "http://" + adminLn.Addr().String()
	do := func(method, path string, body any, token string) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			buf, _ := json.Marshal(body)
			rd = bytes.NewReader(buf)
		}
		req, err := http.NewRequest(method, base+path, rd)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}

	// 1) 鉴权:无 token 401,错误 token 401
	if code, _ := do("GET", "/api/status", nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", code)
	}
	if code, _ := do("GET", "/api/status", nil, "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", code)
	}
	code, body := do("GET", "/api/status", nil, "secret")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %s", code, body)
	}

	// 2) 动态发放凭据 + 路由规则
	if code, body = do("POST", "/api/clientProxies", ClientProxy{
		ClientProxyName: "cp-dyn", StreamId: "s-dyn",
		Addr: backend, Transport: "tcp", KeyPassword: "dyn-pwd",
	}, "secret"); code != http.StatusOK {
		t.Fatalf("add clientProxy = %d, body %s", code, body)
	}
	// 重复添加冲突
	if code, _ = do("POST", "/api/clientProxies", ClientProxy{
		ClientProxyName: "cp-dyn", StreamId: "s-dyn", KeyPassword: "dyn-pwd",
	}, "secret"); code != http.StatusConflict {
		t.Fatalf("duplicate add = %d, want 409", code)
	}
	if code, body = do("POST", "/api/connects", ConnectItem{
		ProxyName: "p0", ClientProxyName: "cp-dyn", StreamId: "s-dyn",
	}, "secret"); code != http.StatusOK {
		t.Fatalf("add connect = %d, body %s", code, body)
	}

	// 3) 新凭据的客户端拨号、注册、被路由
	client, err := star.DialCtrl("tcp", ctrlLn.Addr().String(),
		star.HandshakeReq{ClientName: "dyn-dailer", KeyPassword: "dyn-pwd"},
		[]star.RegisterStreamReq{{ClientProxyName: "cp-dyn", StreamId: "s-dyn"}},
		3*time.Second)
	if err != nil {
		t.Fatalf("DialCtrl with dynamic credential: %v", err)
	}
	defer client.Close()
	client.LocalDialer = func(streamId string) (net.Conn, error) {
		return net.Dial("tcp", backend)
	}
	go func() { _ = client.Run() }()

	want := "HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nok"
	for i := 0; i < 3; i++ {
		if got := proxyRequest(t, proxyLn.Addr().String()); got != want {
			t.Fatalf("request %d: got %q, want %q", i, got, want)
		}
	}

	// 4) 查询:流量统计已累计
	code, body = do("GET", "/api/streams", nil, "secret")
	if code != http.StatusOK {
		t.Fatalf("streams = %d", code)
	}
	var streams []StreamStat
	if err := json.Unmarshal([]byte(body), &streams); err != nil {
		t.Fatalf("decode streams: %v", err)
	}
	if len(streams) != 1 || streams[0].InBytes == 0 || streams[0].OutBytes == 0 {
		t.Fatalf("streams stats = %+v, want traffic counters > 0", streams)
	}

	// 5) 删除路由规则后,新连接无流可用,快速失败
	if code, _ = do("DELETE", "/api/connects/p0/cp-dyn/s-dyn", nil, "secret"); code != http.StatusOK {
		t.Fatalf("delete connect = %d", code)
	}
	if _, err := proxyRequestErr(t, proxyLn.Addr().String()); err == nil {
		t.Fatal("expected request to fail after connect rule removal")
	}

	// 6) 踢下线:流被清理
	if code, body = do("POST", "/api/kick", map[string]string{"clientProxyName": "cp-dyn"}, "secret"); code != http.StatusOK {
		t.Fatalf("kick = %d, body %s", code, body)
	}
	waitFor(t, 3*time.Second, "kick cleanup", func() bool {
		return hub.Streams.Len() == 0
	})

	// 7) 删除凭据后,原密码无法再认证
	if code, _ = do("DELETE", "/api/clientProxies/cp-dyn/s-dyn", nil, "secret"); code != http.StatusOK {
		t.Fatalf("delete clientProxy = %d", code)
	}
	if _, err := star.DialCtrl("tcp", ctrlLn.Addr().String(),
		star.HandshakeReq{KeyPassword: "dyn-pwd"}, nil, 3*time.Second); err == nil {
		t.Fatal("expected revoked credential to be rejected")
	}

	// 8) 凭据列表接口:发放后再查询应可见
	if code, _ = do("POST", "/api/clientProxies", ClientProxy{
		ClientProxyName: "cp-list", StreamId: "s-list", KeyPassword: "pwd-list",
	}, "secret"); code != http.StatusOK {
		t.Fatalf("add clientProxy for list = %d", code)
	}
	code, body = do("GET", "/api/clientProxies", nil, "secret")
	if code != http.StatusOK {
		t.Fatalf("list clientProxies = %d", code)
	}
	// 前端按 camelCase 解析,锁死 JSON 字段名(回归:结构体曾漏配 json tag)
	for _, key := range []string{`"clientProxyName"`, `"streamId"`, `"addr"`, `"keyPassword"`} {
		if !bytes.Contains([]byte(body), []byte(key)) {
			t.Fatalf("clientProxies response missing %s: %s", key, body)
		}
	}
	var cps []ClientProxy
	if err := json.Unmarshal([]byte(body), &cps); err != nil {
		t.Fatalf("decode clientProxies: %v", err)
	}
	found := false
	for _, cp := range cps {
		if cp.ClientProxyName == "cp-list" && cp.StreamId == "s-list" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clientProxies = %+v, want cp-list/s-list", cps)
	}

	// 9) 内嵌 Web 管理台静态托管
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte("rain-net 管理台")) {
		t.Fatalf("GET / = %d, body contains title? %v", resp.StatusCode, bytes.Contains(data, []byte("rain-net 管理台")))
	}
}

// proxyRequestErr 同 proxyRequest,但返回错误供负向用例断言
func proxyRequestErr(t *testing.T, addr string) (string, error) {
	t.Helper()
	ext, err := net.Dial("tcp", addr)
	if err != nil {
		return "", err
	}
	defer ext.Close()
	_ = ext.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := ext.Write([]byte("ping")); err != nil {
		return "", err
	}
	buf := make([]byte, 64)
	n, err := ext.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}
