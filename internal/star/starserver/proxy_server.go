package starserver

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haisentree/rain-net/pluginer"
	"github.com/haisentree/rain-net/protocol/codec"
	"github.com/haisentree/rain-net/protocol/star"
)

// ProxyServer 数据面代理监听器(type: proxy)
//
// 外部用户的每条连接经 connect 规则路由到一条已注册的服务流:
// round-robin 挑选目标 -> hub.OpenConn 请求客户端拨通内网服务 ->
// 双向搬运:外部连接 <-> ctrl 通道数据帧(帧 streamID 字段为连接ID)
type ProxyServer struct {
	Name string
	Net  string
	Addr string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// proxyName 逻辑代理名,匹配 ConnectTable 里的规则
	proxyName string
	// connects 共享的路由规则表,运行时可被 admin API 动态增删
	connects *ConnectTable
	// inCodec 入站协议适配器(raw/ws)
	inCodec codec.Codec

	settings Settings
	hub      *CtrlHub
	rr       atomic.Uint32
}

var _ pluginer.Server = (*ProxyServer)(nil)

func NewProxyServer(serviceName string, listener *ListenerList, connects *ConnectTable, hub *CtrlHub) (*ProxyServer, error) {
	// 入站协议适配器:raw(默认)/ws,协议转化在这一层发生
	c, err := codec.Get(listener.Settings.InCodec)
	if err != nil {
		return nil, err
	}

	return &ProxyServer{
		Name: serviceName,
		Net:  listener.Transport,
		Addr: listener.Addr,

		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,

		proxyName: listener.Settings.ProxyName,
		connects:  connects,
		settings:  listener.Settings,
		hub:       hub,
		inCodec:   c,
	}, nil
}

// pickEntry round-robin 挑选一个当前可用的服务流
func (s *ProxyServer) pickEntry() *StreamEntry {
	targets := s.connects.For(s.proxyName)
	n := len(targets)
	if n == 0 {
		return nil
	}
	start := int(s.rr.Add(1))

	for i := 0; i < n; i++ {
		t := targets[(start+i)%n]
		if t.StreamId != "" {
			if e, ok := s.hub.Streams.GetByName(t.ClientProxyName, t.StreamId); ok {
				return e
			}
			continue
		}
		// 未指定 streamId 时取该客户端代理名下的第一条流
		for _, e := range s.hub.Streams.FindByClientProxy(t.ClientProxyName) {
			return e
		}
	}
	return nil
}

func (s *ProxyServer) Listen() (net.Listener, error) {
	if s.Net != "tcp" {
		return nil, nil
	}
	return listenTCP(s.Addr, s.settings)
}

func (s *ProxyServer) ListenPacket() (net.PacketConn, error) {
	// 数据面当前只走 TCP
	return nil, nil
}

func (s *ProxyServer) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *ProxyServer) ServePacket(p net.PacketConn) error {
	// 数据面当前只走 TCP
	return nil
}

func (s *ProxyServer) handleConn(conn net.Conn) {
	// 入站协议适配:raw 直接包装,ws 等在此完成协议握手
	mc, err := s.inCodec.Accept(conn)
	if err != nil {
		slog.Warn("proxy codec accept", "listener", s.Name, "err", err)
		conn.Close()
		return
	}

	entry := s.pickEntry()
	if entry == nil {
		slog.Warn("proxy no available stream", "listener", s.Name, "remote", conn.RemoteAddr())
		mc.Close()
		return
	}

	e, err := s.hub.OpenConn(entry)
	if err != nil {
		slog.Warn("proxy open conn", "listener", s.Name, "err", err)
		mc.Close()
		return
	}
	slog.Info("proxy conn via stream", "conn", e.ConnID, "stream", e.StreamId,
		"codec", s.inCodec.Name(), "remote", conn.RemoteAddr())

	// 生命周期与 R2 Bridge 状态机同构:两个方向独立收尾,
	// 双方向都结束(或任一方向出错)才整条拆除。
	// 上行 EOF → 半关帧通知客户端;下行半关帧 → 排空后对外部 CloseWrite(支持则)。
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	var (
		teardownOnce sync.Once
		upDone       atomic.Bool
		downDone     atomic.Bool
	)
	teardown := func() {
		teardownOnce.Do(func() {
			stop()
			s.hub.CloseConn(e.ConnID, true)
			mc.Close()
		})
	}
	defer teardown()
	bothFinished := func() {
		if upDone.Load() && downDone.Load() {
			teardown()
		}
	}

	// 下行泵:ctrl 通道 -> 外部连接(按入站协议编码)
	go func() {
		defer func() { stop() }()
		for {
			select {
			case payload := <-e.sink:
				if err := mc.WriteMessage(payload); err != nil {
					teardown()
					return
				}
			case <-e.remoteClosed:
				// 客户端内网侧写完:按序排空在途数据
				for {
					select {
					case payload := <-e.sink:
						if err := mc.WriteMessage(payload); err != nil {
							teardown()
							return
						}
					default:
						downDone.Store(true)
						// 支持半关闭则只关外部写侧,外部仍可继续上行;否则降级整条关闭
						if cw, ok := mc.(interface{ CloseWrite() error }); ok && cw.CloseWrite() == nil {
							bothFinished()
							return
						}
						teardown()
						return
					}
				}
			case <-e.done:
				teardown()
				return
			case <-done:
				return
			}
		}
	}()

	// 上行:外部连接 -> ctrl 通道(按入站协议解码,ws 消息保留边界)
	for {
		payload, err := mc.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// 外部用户不再上行:半关帧通知客户端(内网连接关写),
				// 本方向结束,等下行泵收尾
				upDone.Store(true)
				_ = e.Client.Send(star.HalfClose(e.ConnID))
				bothFinished()
				<-done
				return
			}
			return // defer teardown 整条拆除
		}
		if err := e.Client.Send(&star.Message{
			Type:     star.MsgData,
			StreamID: e.ConnID,
			Payload:  payload,
		}); err != nil {
			return
		}
		if e.stream != nil {
			e.stream.InBytes.Add(uint64(len(payload)))
		}
	}
}
