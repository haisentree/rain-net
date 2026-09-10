package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"rain-net/internal/star/starserver"
	"rain-net/protocol/star"

	"gopkg.in/yaml.v3"
)

// starclient 客户端(dailer):拨号到 ctrlproxy 监听器,握手认证 + 流注册
// + 保活,断线自动重连,并把外部连接转发到内网服务
//
// 两种用法:
//
//  1. 配置文件模式(推荐,与服务端共用 etc/star.example.yaml):
//     go run ./cmd/starclient -config etc/star.example.yaml -name dailer-0
//     从 dailerList 取地址/密码/TLS,从 clientProxy 取内网服务地址
//
//  2. 手工模式(仅控制面,不转发数据):
//     go run ./cmd/starclient -addr 127.0.0.1:5172 -password xxx \
//     -streams clientProxy-0:dailer-0-stream-1
func main() {
	var (
		config    = flag.String("config", "", "star 配置文件,读取 dailer/clientProxy 定义")
		addr      = flag.String("addr", "", "ctrlproxy 服务地址(缺省取配置文件 dailer.addr)")
		password  = flag.String("password", "", "握手密码 keyPassword(缺省取配置文件)")
		name      = flag.String("name", "dailer-0", "客户端名称,配置文件模式下对应 dailerList.name")
		streamStr = flag.String("streams", "clientProxy-0:dailer-0-stream-1",
			"手工模式的注册流,格式 clientProxyName:streamId,逗号分隔")
		keepalive = flag.Duration("keepalive", 10*time.Second, "保活间隔")
		tlsMode   = flag.Bool("tls", false, "手工模式:与 TLS 监听器建立隧道")
		tlsSkip   = flag.Bool("tlsSkipVerify", false, "手工模式:跳过服务端证书校验")
	)
	flag.Parse()

	var (
		streams      []star.RegisterStreamReq
		localTargets map[string]string // streamId -> 内网服务地址
	)

	if *config != "" {
		data, err := os.ReadFile(*config)
		if err != nil {
			fmt.Println("读取配置失败:", err)
			os.Exit(1)
		}
		var cfg starserver.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Println("解析配置失败:", err)
			os.Exit(1)
		}

		var dailer *starserver.DailerList
		for i := range cfg.DailerList {
			if cfg.DailerList[i].Name == *name {
				dailer = &cfg.DailerList[i]
				break
			}
		}
		if dailer == nil {
			fmt.Println("配置中找不到 dailer:", *name)
			os.Exit(1)
		}

		// 内网服务地址:clientProxyName 匹配的 clientProxy 条目
		localTargets = make(map[string]string)
		for _, l := range cfg.ListenerList {
			for _, cp := range l.Settings.ClientProxy {
				if cp.ClientProxyName != dailer.ClientProxyName {
					continue
				}
				streams = append(streams, star.RegisterStreamReq{
					ClientProxyName: cp.ClientProxyName,
					StreamId:        cp.StreamId,
				})
				localTargets[cp.StreamId] = cp.Addr
			}
		}

		if *password == "" {
			*password = dailer.KeyPassword
		}
		if *addr == "" {
			*addr = dailer.Addr
		}

		dialer := starserver.NewDialer(*name, *addr, *password, streams)
		dialer.TLS = dailer.TLS
		dialer.TLSSkipVerify = dailer.TLSSkipVerify
		dialer.KeepAlive = *keepalive
		dialer.LocalDialer = localDialer(localTargets)

		go func() {
			<-sigChan()
			fmt.Println("退出,关闭连接")
			dialer.Close()
		}()
		if err := dialer.Run(); err != nil {
			fmt.Println("拨号器退出:", err)
			os.Exit(1)
		}
		return
	}

	// 手工模式
	if *password == "" {
		fmt.Println("必须提供 -password 或 -config")
		os.Exit(1)
	}
	for _, part := range strings.Split(*streamStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			fmt.Println("非法流定义(应为 clientProxyName:streamId):", part)
			os.Exit(1)
		}
		streams = append(streams, star.RegisterStreamReq{ClientProxyName: kv[0], StreamId: kv[1]})
	}

	dialer := starserver.NewDialer(*name, *addr, *password, streams)
	dialer.KeepAlive = *keepalive
	dialer.TLS = *tlsMode
	dialer.TLSSkipVerify = *tlsSkip

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

// localDialer 按 streamId 映射拨号内网服务
func localDialer(targets map[string]string) func(string) (net.Conn, error) {
	if len(targets) == 0 {
		return nil
	}
	return func(streamId string) (net.Conn, error) {
		target, ok := targets[streamId]
		if !ok {
			return nil, fmt.Errorf("stream %q has no local target", streamId)
		}
		return net.Dial("tcp", target)
	}
}

func sigChan() chan os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return sig
}
