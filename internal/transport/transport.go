// Package transport 定义传输层插件:只负责把字节从 A 搬到 B,不携带任何协议语义。
//
// 约定(docs/design.md §二):
//   - Transport 是数据面唯一的传输抽象,Listen/Dial 统一返回 net.Listener/net.Conn;
//   - 多路复用传输(h2/quic/star 等)在实现内部私有化连接池,Dial 返回逻辑流,接口不变;
//   - v1 只做流式传输,不建模 PacketConn(UDP 数据报),udp 在装配层走 legacy 特例。
package transport

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
)

// Options 是装配层传给各传输工厂的构建参数。
// 字段按需取用,各传输自行忽略不相关项;后续新传输的专属配置经扩展结构或 metadata 加入。
type Options struct {
	CertFile      string // TLS 证书
	KeyFile       string // TLS 私钥
	TLSSkipVerify bool   // 出站跳过证书校验(自签名场景)
	Path          string // ws 等基于 HTTP 的传输:握手路径(默认 "/")
}

// Transport 是传输层唯一接口。
type Transport interface {
	// Listen 在 addr 上监听,返回标准 net.Listener(accept 出的连接即字节流)。
	// ctx 取消时应关闭 listener 并使 Accept 返回错误。
	Listen(ctx context.Context, addr string) (net.Listener, error)
	// Dial 连接 addr,返回标准 net.Conn。
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

// BuildFunc 按 Options 构建一个 Transport 实例。
type BuildFunc func(opts Options) (Transport, error)

var (
	mu       sync.RWMutex
	registry = map[string]BuildFunc{}
)

// Register 注册一个传输工厂,重名 panic(装配期错误,尽早暴露)。
func Register(name string, fn BuildFunc) {
	if name == "" {
		panic("transport: name must not be empty")
	}
	if fn == nil {
		panic("transport: build func must not be nil")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic("transport: duplicate registration for " + name)
	}
	registry[name] = fn
}

// Build 按名称与 Options 构建传输实例。
func Build(name string, opts Options) (Transport, error) {
	mu.RLock()
	fn, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("transport: unknown transport %q (registered: %v)", name, Names())
	}
	return fn(opts)
}

// Names 返回已注册的传输名(排序,用于报错与调试)。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// WatchClose 落实 Listen 的 ctx 契约:ctx 取消时关闭 listener。
// net.ListenConfig.Listen 只在创建阶段尊重 ctx,创建后需传输实现自行接管;
// 各传输实现在 Listen 成功后调用本函数。ctx 无 Done 通道(如 Background)时原样返回。
func WatchClose(ctx context.Context, ln net.Listener) net.Listener {
	done := ctx.Done()
	if done == nil {
		return ln
	}
	go func() {
		<-done
		ln.Close()
	}()
	return ln
}
