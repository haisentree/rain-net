package starserver

import (
	"crypto/tls"
	"log/slog"
	"net"
	"sync"
	"time"

	"rain-net/protocol/star"
)

// 重连退避参数:首次 1 秒,每次翻倍,封顶 30 秒;连接成功后复位
const (
	reconnectBase = 1 * time.Second
	reconnectMax  = 30 * time.Second
	dialTimeout   = 5 * time.Second
)

// Dialer 客户端拨号器(dailerList 对应的运行时组件):
// 建立 ctrl 通道会话,断线后按指数退避自动重连
//
// 用法:go dialer.Run() 阻塞运行;Close 停止并退出 Run
type Dialer struct {
	Name        string // 客户端名称(握手 ClientName)
	Addr        string // ctrl 通道地址
	KeyPassword string // 握手密码,对应 clientProxy.keyPassword
	Streams     []star.RegisterStreamReq

	// LocalDialer 服务端请求建立连接时,按 streamId 拨号内网服务
	LocalDialer func(streamId string) (net.Conn, error)

	TLS           bool          // 与服务端建立 TLS 隧道
	TLSSkipVerify bool          // 跳过服务端证书校验(自签名证书场景)
	KeepAlive     time.Duration // 保活间隔,0 表示不保活

	mu     sync.Mutex
	client *star.CtrlClient

	stopCh chan struct{}
	once   sync.Once
}

func NewDialer(name, addr, keyPassword string, streams []star.RegisterStreamReq) *Dialer {
	return &Dialer{
		Name:        name,
		Addr:        addr,
		KeyPassword: keyPassword,
		Streams:     streams,
		stopCh:      make(chan struct{}),
	}
}

// Client 返回当前会话,未连接时为 nil
func (d *Dialer) Client() *star.CtrlClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.client
}

// Run 阻塞运行:连接 -> 会话读循环 -> 断线重连,Close 后返回 nil
func (d *Dialer) Run() error {
	backoff := reconnectBase
	for {
		select {
		case <-d.stopCh:
			return nil
		default:
		}

		client, err := d.dial()
		if err != nil {
			slog.Warn("dailer connect failed", "name", d.Name, "addr", d.Addr, "err", err, "retryIn", backoff.String())
			select {
			case <-d.stopCh:
				return nil
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, reconnectMax)
			continue
		}
		backoff = reconnectBase

		d.mu.Lock()
		d.client = client
		d.mu.Unlock()

		slog.Info("dailer connected", "name", d.Name, "addr", d.Addr, "streams", len(d.Streams))

		client.LocalDialer = d.LocalDialer
		if d.KeepAlive > 0 {
			client.StartKeepAlive(d.KeepAlive)
		}
		err = client.Run()
		client.Close()

		d.mu.Lock()
		d.client = nil
		d.mu.Unlock()

		slog.Warn("dailer disconnected", "name", d.Name, "err", err)

		// 断开后稍作停顿再进入下一轮重连
		select {
		case <-d.stopCh:
			return nil
		case <-time.After(reconnectBase):
		}
	}
}

func (d *Dialer) dial() (*star.CtrlClient, error) {
	var tlsCfg *tls.Config
	if d.TLS {
		tlsCfg = &tls.Config{InsecureSkipVerify: d.TLSSkipVerify}
	}
	req := star.HandshakeReq{
		ClientName:  d.Name,
		KeyPassword: d.KeyPassword,
	}
	return star.DialCtrlTLS("tcp", d.Addr, req, d.Streams, dialTimeout, tlsCfg)
}

// Close 停止重连循环并断开当前会话
func (d *Dialer) Close() {
	d.once.Do(func() { close(d.stopCh) })

	d.mu.Lock()
	client := d.client
	d.mu.Unlock()
	if client != nil {
		client.Close()
	}
}
