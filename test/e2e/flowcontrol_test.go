package e2e

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/client"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/star/starserver"
	"github.com/haisentree/rain-net/protocol/star"
)

// TestFlowControlNoHOL 每流流控(R8 旗舰验证):
// 慢流(对端不读)占满自身发送窗口后,同会话上的快流必须不受影响。
// 修复前:客户端读循环在慢流写上阻塞,快流饿死。
func TestFlowControlNoHOL(t *testing.T) {
	// 快流后端:echo
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				c.Close()
			}(c)
		}
	}()

	hub := starhandler.NewCtrlHub()

	ctrlSettings := config.Settings{
		ClientProxy: []config.ClientProxy{
			{ClientProxyName: "clientProxy-0", StreamId: "stream-slow", KeyPassword: "pwd"},
			{ClientProxyName: "clientProxy-0", StreamId: "stream-fast", KeyPassword: "pwd"},
		},
	}
	ctrl := starhandler.NewCtrlProxyServer("svc", &config.ListenerList{
		Type: "ctrlproxy", Transport: "tcp", Settings: ctrlSettings,
	}, hub)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()
	go func() { _ = ctrl.Serve(ctrlLn) }()

	// 两个确定性代理监听器:各绑一条流(避免 round-robin 分配的不确定性)
	slowTable := starserver.NewConnectTable()
	slowTable.Seed([]config.ConnectItem{
		{ProxyName: "p-slow", ClientProxyName: "clientProxy-0", StreamId: "stream-slow"},
	})
	proxySlow, perr := starserver.NewProxyServer("svc", &config.ListenerList{
		Type: "proxy", Transport: "tcp",
		Settings: config.Settings{ProxyName: "p-slow"},
	}, slowTable, hub)
	if perr != nil {
		t.Fatal(perr)
	}
	slowLn, err := proxySlow.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer slowLn.Close()
	go func() { _ = proxySlow.Serve(slowLn) }()

	fastTable := starserver.NewConnectTable()
	fastTable.Seed([]config.ConnectItem{
		{ProxyName: "p-fast", ClientProxyName: "clientProxy-0", StreamId: "stream-fast"},
	})
	proxyFast, perr := starserver.NewProxyServer("svc", &config.ListenerList{
		Type: "proxy", Transport: "tcp",
		Settings: config.Settings{ProxyName: "p-fast"},
	}, fastTable, hub)
	if perr != nil {
		t.Fatal(perr)
	}
	fastLn, err := proxyFast.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer fastLn.Close()
	go func() { _ = proxyFast.Serve(fastLn) }()

	// 隧道客户端:慢流 → 同步管道(写阻塞 = 永不消费);快流 → echo
	dialer := client.NewDialer("dailer-0", ctrlLn.Addr().String(), "pwd",
		[]star.RegisterStreamReq{
			{ClientProxyName: "clientProxy-0", StreamId: "stream-slow"},
			{ClientProxyName: "clientProxy-0", StreamId: "stream-fast"},
		})
	dialer.LocalDialer = func(streamId string) (net.Conn, error) {
		if streamId == "stream-slow" {
			slow, _ := net.Pipe() // 无读者:写阻塞
			t.Cleanup(func() { slow.Close() })
			return slow, nil
		}
		return net.Dial("tcp", echoLn.Addr().String())
	}
	go func() { _ = dialer.Run() }()
	defer dialer.Close()
	waitFor(t, 5*time.Second, "streams registered", func() bool {
		return hub.Streams.Len() == 2
	})

	// 慢流外部连接:灌 1MB(远超 256KB 窗口)后保持不读
	slowExt, err := net.Dial("tcp", slowLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer slowExt.Close()
	slowExt.Write(make([]byte, 1<<20)) // TCP 缓冲吸收,代理侧窗口截停

	// 快流外部连接:必须正常回环(不被慢流拖死)
	fastExt, err := net.Dial("tcp", fastLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer fastExt.Close()
	if err := fastExt.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("fast-stream-ping")
	if _, err := fastExt.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(fastExt, buf); err != nil {
		t.Fatalf("fast stream blocked by slow stream (HOL): %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("echo mismatch: %q", buf)
	}
	_ = slowExt
}
