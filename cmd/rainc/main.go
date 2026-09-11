package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/client"
	"github.com/haisentree/rain-net/protocol/star"
)

// rainc 客户端(隧道拨号器):领取配置中一个 dialer 定义,连到服务端 star 入口,
// 注册身份名下的流并保活,把服务端转来的连接转发到本地内网服务。
//
// 用法:
//
//	rainc -config etc/star.example.yaml -dialer office-0
//	rainc -addr 127.0.0.1:5172 -identity clientProxy-0 -password pwd //	      -streams clientProxy-0:stream-1        # 手工模式(仅控制面注册)
func main() {
	var (
		configFile = flag.String("config", "", "配置文件,读取 dialers 定义")
		dialerName = flag.String("dialer", "", "配置文件模式下领取的 dialer 名")
		addr       = flag.String("addr", "", "服务端 star 入口地址(覆盖配置)")
		identity   = flag.String("identity", "", "客户端身份(覆盖配置)")
		password   = flag.String("password", "", "接入凭据 keyPassword(覆盖配置)")
		streamStr  = flag.String("streams", "",
			"手工模式的注册流,格式 identity:streamId,逗号分隔")
		keepalive = flag.Duration("keepalive", 10*time.Second, "保活间隔")
		tlsMode   = flag.Bool("tls", false, "手工模式:与 TLS 监听器建立隧道")
		tlsSkip   = flag.Bool("tlsSkipVerify", false, "手工模式:跳过服务端证书校验")
	)
	flag.Parse()

	var (
		streams      []star.RegisterStreamReq
		localTargets map[string]string
	)

	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			fmt.Println("读取配置失败:", err)
			os.Exit(1)
		}
		root, err := config.Load(data)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		var d *config.DialerSpec
		for i := range root.Dialers {
			if root.Dialers[i].Name == *dialerName {
				d = &root.Dialers[i]
				break
			}
		}
		if d == nil {
			fmt.Println("配置中找不到 dialer:", *dialerName)
			os.Exit(1)
		}

		if *addr == "" {
			*addr = d.Addr
		}
		if *identity == "" {
			*identity = d.Identity
		}
		if *password == "" {
			*password = d.KeyPassword
		}
		*dialerName = d.Name
		*tlsMode = d.Transport == "tls"
		localTargets = make(map[string]string, len(d.Locals))
		for _, loc := range d.Locals {
			streams = append(streams, star.RegisterStreamReq{
				ClientProxyName: d.Identity, StreamId: loc.Stream,
			})
			localTargets[loc.Stream] = loc.Addr
		}
		runDialer(dialerArgs{
			name: *dialerName, addr: *addr, identity: *identity, password: *password,
			streams: streams, keepalive: *keepalive, tls: *tlsMode, tlsSkip: *tlsSkip,
			localTargets: localTargets,
		})
		return
	}

	// 手工模式(仅控制面注册,不转发数据)
	if *password == "" || *identity == "" {
		fmt.Println("手工模式必须提供 -identity 与 -password(或使用 -config)")
		os.Exit(1)
	}
	for _, part := range splitCSV(*streamStr) {
		kv := split2(part, ":")
		streams = append(streams, star.RegisterStreamReq{ClientProxyName: kv[0], StreamId: kv[1]})
	}
	runDialer(dialerArgs{
		name: *identity, addr: *addr, identity: *identity, password: *password,
		streams: streams, keepalive: *keepalive, tls: *tlsMode, tlsSkip: *tlsSkip,
	})
}

type dialerArgs struct {
	name, addr, identity, password string
	streams                        []star.RegisterStreamReq
	keepalive                      time.Duration
	tls, tlsSkip                   bool
	localTargets                   map[string]string
}

func runDialer(a dialerArgs) {
	dialer := client.NewDialer(a.name, a.addr, a.password, a.streams)
	dialer.TLS = a.tls
	dialer.TLSSkipVerify = a.tlsSkip
	dialer.KeepAlive = a.keepalive
	if len(a.localTargets) > 0 {
		dialer.LocalDialer = func(streamId string) (net.Conn, error) {
			target, ok := a.localTargets[streamId]
			if !ok {
				return nil, fmt.Errorf("stream %q has no local target", streamId)
			}
			return net.Dial("tcp", target)
		}
	}

	go func() {
		<-sigChan()
		fmt.Println("退出,关闭连接")
		dialer.Close()
	}()
	if err := dialer.Run(); err != nil {
		fmt.Println("拨号器退出:", err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func split2(s, sep string) [2]string {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return [2]string{s[:i], s[i+len(sep):]}
		}
	}
	return [2]string{s, ""}
}

func sigChan() chan os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return sig
}
