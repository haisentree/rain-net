package star

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

// uplinkChunk 客户端上行(内网服务 -> ctrl 通道)的读取块大小
const uplinkChunk = 32 * 1024

// CtrlClient ctrl 控制通道的客户端侧封装(dailer 使用)
//
// DialCtrl 完成拨号 -> 握手认证 -> 批量注册流,之后:
//   - Run 阻塞运行读循环:处理服务端的连接建立请求(经 LocalDialer 拨通
//     内网服务)、数据帧双向转发、ping/pong
//   - StartKeepAlive 周期性 ping 保活
//
// 设置 LocalDialer 后即具备完整的数据面能力(反向代理客户端);
// 不设置时只工作在控制面(注册流/保活),连接建立请求会被拒绝
type CtrlClient struct {
	sess *Session

	mu        sync.RWMutex
	streams   map[string]uint32       // 配置里的字符串 streamId -> 服务端分配的数字流ID
	streamIDs map[uint32]string       // 数字流ID -> 字符串 streamId
	pairs     map[string]struct{}     // 已注册的 clientProxyName\x00streamId 身份对
	conns     map[uint32]rainnet.Conn // 连接ID -> 内网服务连接(半关闭语义经 Conn 传播)

	// 节点串联(R7):客户端主动拨号建立的连接(connID 高位命名空间)
	dialMu    sync.Mutex
	dialConns map[uint32]*StreamConn
	dialAcks  map[uint32]chan ClientConnAck
	dialSeq   uint32

	// LocalDialer 服务端请求建立连接时,按 streamId 拨号内网服务
	LocalDialer func(streamId string) (net.Conn, error)

	// OnDisconnect 连接断开时回调一次
	OnDisconnect func(err error)

	closeOnce sync.Once
	closed    chan struct{}
}

// DialCtrl 拨号并完成握手与流注册,任一步失败返回 error 并关闭连接
// dialTimeout 只约束建立连接,握手/注册有协议内的 10 秒超时
func DialCtrl(network, addr string, req HandshakeReq, streams []RegisterStreamReq, dialTimeout time.Duration) (*CtrlClient, error) {
	return DialCtrlTLS(network, addr, req, streams, dialTimeout, nil)
}

// DialCtrlTLS 同 DialCtrl,tlsConfig 非 nil 时使用 TLS 建立隧道
func DialCtrlTLS(network, addr string, req HandshakeReq, streams []RegisterStreamReq, dialTimeout time.Duration, tlsConfig *tls.Config) (*CtrlClient, error) {
	dialer := net.Dialer{Timeout: dialTimeout}

	var conn net.Conn
	var err error
	if tlsConfig != nil {
		cfg := tlsConfig.Clone()
		// 未指定 ServerName 时取目标地址的主机部分(配合 InsecureSkipVerify 可省)
		if cfg.ServerName == "" {
			host, _, splitErr := net.SplitHostPort(addr)
			if splitErr == nil {
				cfg.ServerName = host
			}
		}
		conn, err = tls.DialWithDialer(&dialer, network, addr, cfg)
	} else {
		conn, err = dialer.Dial(network, addr)
	}
	if err != nil {
		return nil, fmt.Errorf("star: dial ctrl %s: %w", addr, err)
	}

	c := &CtrlClient{
		sess:      NewSession(conn),
		streams:   make(map[string]uint32, len(streams)),
		streamIDs: make(map[uint32]string, len(streams)),
		pairs:     make(map[string]struct{}, len(streams)),
		conns:     make(map[uint32]rainnet.Conn),
		dialConns: make(map[uint32]*StreamConn),
		dialAcks:  make(map[uint32]chan ClientConnAck),
		closed:    make(chan struct{}),
	}

	ack, err := ClientHandshake(c.sess, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("star: handshake with %s: %w", addr, err)
	}
	if !ack.OK {
		conn.Close()
		return nil, fmt.Errorf("star: handshake with %s rejected: %s", addr, ack.Message)
	}

	for _, st := range streams {
		rack, err := ClientRegisterStream(c.sess, st)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("star: register stream %s: %w", st.StreamId, err)
		}
		if !rack.OK {
			conn.Close()
			return nil, fmt.Errorf("star: register stream %s rejected: %s", st.StreamId, rack.Message)
		}
		c.streams[st.StreamId] = rack.AssignID
		c.streamIDs[rack.AssignID] = st.StreamId
		c.pairs[st.ClientProxyName+"\x00"+st.StreamId] = struct{}{}
	}

	return c, nil
}

// StreamID 查询字符串 streamId 对应的数字流ID
func (c *CtrlClient) StreamID(streamId string) (uint32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.streams[streamId]
	return id, ok
}

// Streams 返回已注册流的快照
func (c *CtrlClient) Streams() map[string]uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]uint32, len(c.streams))
	for k, v := range c.streams {
		out[k] = v
	}
	return out
}

// SendData 发送数据帧
func (c *CtrlClient) SendData(streamID uint32, payload []byte) error {
	return c.sess.Send(&Message{Type: MsgData, StreamID: streamID, Payload: payload})
}

// CloseStream 通知服务端关闭一条流
func (c *CtrlClient) CloseStream(streamID uint32) error {
	return c.sess.Send(&Message{Type: MsgStreamClose, StreamID: streamID})
}

// StartKeepAlive 启动周期性 ping,连接关闭后自动退出
func (c *CtrlClient) StartKeepAlive(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
				if err := c.sess.Send(&Message{Type: MsgPing, StreamID: CtrlStreamID}); err != nil {
					return
				}
			}
		}
	}()
}

// Run 阻塞运行读循环,连接断开后返回
func (c *CtrlClient) Run() error {
	var runErr error
	defer func() {
		c.closeOnce.Do(func() { close(c.closed) })
		c.closeAllConns()
		if c.OnDisconnect != nil {
			c.OnDisconnect(runErr)
		}
	}()

	for {
		msg, err := c.sess.Receive()
		if err != nil {
			runErr = err
			return err
		}

		switch msg.Type {
		case MsgConnOpen:
			c.handleConnOpen(msg)
		case MsgData:
			if sc := c.dialConn(msg.StreamID); sc != nil {
				sc.deliver(feedItem{data: msg.Payload})
				continue
			}
			c.writeConn(msg.StreamID, msg.Payload)
		case MsgStreamClose:
			if sc := c.dialConn(msg.StreamID); sc != nil {
				sc.deliver(feedItem{half: msg.IsHalfClose(), full: !msg.IsHalfClose()})
				c.removeDialConn(msg.StreamID)
				continue
			}
			// 半关 = 服务端不再上行,内网连接写侧关闭(排空语义由对端负责),
			// 连接保留,上行泵继续;全关 = 整条终止。notify=false 避免回发关流帧
			if msg.IsHalfClose() {
				c.halfCloseConn(msg.StreamID)
			} else {
				c.closeConn(msg.StreamID, false)
			}
		case MsgPing:
			// 服务端探测保活,原样回 pong
			if err := c.sess.Send(&Message{Type: MsgPong, StreamID: msg.StreamID, Payload: msg.Payload}); err != nil {
				runErr = err
				return err
			}
		case MsgClientConnAck:
			var ack ClientConnAck
			if err := DecodeControl(msg, &ack); err != nil {
				slog.Warn("bad clientConnAck", "err", err)
				continue
			}
			c.dialMu.Lock()
			ch, ok := c.dialAcks[ack.ConnID]
			delete(c.dialAcks, ack.ConnID)
			c.dialMu.Unlock()
			if ok {
				ch <- ack
			}
		case MsgPong:
			// StartKeepAlive 的应答,当前不统计 RTT
		default:
			slog.Debug("unexpected message", "type", msg.TypeName(), "stream", msg.StreamID)
		}
	}
}

// handleConnOpen 处理服务端的连接建立请求:
// 按 streamId 拨号内网服务,回 ack,成功后启动上行泵
func (c *CtrlClient) handleConnOpen(msg *Message) {
	var req ConnOpenReq
	if err := DecodeControl(msg, &req); err != nil {
		slog.Warn("bad connOpen", "err", err)
		return
	}

	ack := ConnOpenAck{ConnID: req.ConnID}
	var conn net.Conn
	if !c.hasStream(req.StreamID) {
		ack.Message = fmt.Sprintf("unknown stream %d", req.StreamID)
	} else if c.LocalDialer == nil {
		ack.Message = "local dialer not set"
	} else {
		c.mu.RLock()
		streamId := c.streamIDs[req.StreamID]
		c.mu.RUnlock()

		var err error
		conn, err = c.LocalDialer(streamId)
		if err != nil {
			ack.Message = fmt.Sprintf("dial %q: %v", streamId, err)
		} else {
			ack.OK = true
		}
	}

	if err := c.sess.SendControl(MsgConnOpenAck, ack); err != nil {
		slog.Warn("send connOpenAck", "err", err)
		if conn != nil {
			conn.Close()
		}
		return
	}
	if conn == nil {
		slog.Info("conn refused", "conn", req.ConnID, "reason", ack.Message)
		return
	}

	rc := rainnet.Wrap(conn, rainnet.Meta{})
	c.mu.Lock()
	c.conns[req.ConnID] = rc
	c.mu.Unlock()

	go c.pumpUplink(req.ConnID, rc)
}

// pumpUplink 内网服务 -> ctrl 通道方向:读到多少发多少。
// 读到 EOF(内网侧写完)发半关帧,连接保留——内网仍可继续收上行数据;
// 其他错误整条关闭并通知服务端。
func (c *CtrlClient) pumpUplink(connID uint32, conn rainnet.Conn) {
	buf := make([]byte, uplinkChunk)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if err := c.SendData(connID, buf[:n]); err != nil {
				break
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = c.sess.Send(HalfClose(connID))
			} else {
				c.closeConn(connID, true)
			}
			return
		}
	}
	// conn 已被本地 Close:通知服务端整条终止
	c.closeConn(connID, true)
}

// halfCloseConn 服务端半关闭:内网连接写侧关闭,连接保留
func (c *CtrlClient) halfCloseConn(connID uint32) {
	c.mu.RLock()
	conn := c.conns[connID]
	c.mu.RUnlock()
	if conn == nil {
		return
	}
	// 本地连接不支持半关闭时无法通知内网,保持现状(降级)
	_ = conn.CloseWrite()
}

// writeConn 服务端下行数据写入内网连接
func (c *CtrlClient) writeConn(connID uint32, payload []byte) {
	c.mu.RLock()
	conn := c.conns[connID]
	c.mu.RUnlock()
	if conn == nil {
		slog.Debug("data for unknown conn", "conn", connID)
		return
	}
	if _, err := conn.Write(payload); err != nil {
		c.closeConn(connID, true)
	}
}

// closeConn 关闭并移除一条连接,notify 为 true 时通知服务端
func (c *CtrlClient) closeConn(connID uint32, notify bool) {
	c.mu.Lock()
	conn := c.conns[connID]
	delete(c.conns, connID)
	c.mu.Unlock()

	if conn == nil {
		return
	}
	conn.Close()
	if notify {
		_ = c.sess.Send(&Message{Type: MsgStreamClose, StreamID: connID})
	}
}

// dialConn 查询节点串联连接
func (c *CtrlClient) dialConn(connID uint32) *StreamConn {
	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	return c.dialConns[connID]
}

// removeDialConn 移除节点串联连接
func (c *CtrlClient) removeDialConn(connID uint32) {
	c.dialMu.Lock()
	delete(c.dialConns, connID)
	c.dialMu.Unlock()
}

// DialIntranet 节点串联:请求服务端在已注册流上建立一条到内网目标的连接,
// 返回的连接即"穿过隧道的内网连接"(半关闭语义经隧道传播)。
// connID 使用高位命名空间,与服务端分配的连接 ID 天然隔离。
func (c *CtrlClient) DialIntranet(ctx context.Context, clientProxyName, streamId string) (net.Conn, error) {
	if _, ok := c.pairs[clientProxyName+"\x00"+streamId]; !ok {
		return nil, fmt.Errorf("star: stream %s/%s not registered by this client", clientProxyName, streamId)
	}

	c.dialMu.Lock()
	if c.dialSeq >= 0x7fffffff {
		c.dialMu.Unlock()
		return nil, fmt.Errorf("star: dial conn id exhausted")
	}
	c.dialSeq++
	connID := uint32(0x80000000) | c.dialSeq
	sc := newStreamConn(c.sess, connID)
	c.dialConns[connID] = sc
	ackCh := make(chan ClientConnAck, 1)
	c.dialAcks[connID] = ackCh
	c.dialMu.Unlock()

	cleanup := func() {
		c.dialMu.Lock()
		delete(c.dialConns, connID)
		delete(c.dialAcks, connID)
		c.dialMu.Unlock()
	}

	err := c.sess.SendControl(MsgClientConnReq, ClientConnReq{
		ConnID: connID, ClientProxyName: clientProxyName, StreamId: streamId,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("star: send clientConnReq: %w", err)
	}

	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case ack := <-ackCh:
		if ack.OK {
			return sc, nil
		}
		cleanup()
		return nil, fmt.Errorf("star: dial intranet %s/%s rejected: %s", clientProxyName, streamId, ack.Message)
	case <-ctx2.Done():
		cleanup()
		return nil, fmt.Errorf("star: dial intranet %s/%s: %w", clientProxyName, streamId, ctx2.Err())
	}
}

func (c *CtrlClient) closeAllConns() {
	c.mu.Lock()
	conns := c.conns
	c.conns = make(map[uint32]rainnet.Conn)
	c.mu.Unlock()

	for _, conn := range conns {
		conn.Close()
	}

	c.dialMu.Lock()
	dials := c.dialConns
	c.dialConns = make(map[uint32]*StreamConn)
	c.dialMu.Unlock()
	for _, sc := range dials {
		sc.markClosed(io.EOF)
	}
}

func (c *CtrlClient) hasStream(streamID uint32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.streamIDs[streamID]
	return ok
}

// Close 主动关闭连接,Run 会随之返回
func (c *CtrlClient) Close() error {
	return c.sess.Close()
}
