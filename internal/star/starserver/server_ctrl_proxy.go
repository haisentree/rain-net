package starserver

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"rain-net/pluginer"
	"rain-net/protocol/star"
)

// clientInfo 在线客户端的运行时信息
type clientInfo struct {
	Name     string
	Identity string
	Remote   string
	Since    time.Time
}

// ClientInfo 客户端信息的只读快照,用于 API 查询
type ClientInfo struct {
	Name     string    `json:"name"`
	Identity string    `json:"identity"`
	Remote   string    `json:"remote"`
	Since    time.Time `json:"since"`
	Streams  int       `json:"streams"`
}

// CtrlProxyServer ctrl 控制通道监听器(type: ctrlproxy)
//
// 客户端(dailer)拨号进来后:握手认证 -> 注册流(进 hub.Streams) -> 消息分发。
// 数据帧经 hub.Downlink 路由到对应外部连接,连接建立应答经
// hub.DeliverConnAck 唤醒等待中的 proxy 监听器
//
// 多客户端隔离:clientProxy 条目的 keyPassword 各自对应一个客户端身份,
// 握手通过后该会话只能注册自己身份名下的 clientProxyName;
// settings.keyPassword 为管理员密码,不限注册身份
type CtrlProxyServer struct {
	Name string
	Net  string
	Addr string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// settings 与 ListenerMap 共享的指针:admin API 的运行时修改即时生效
	settings *Settings
	hub      *CtrlHub

	// credMu 保护 settings.ClientProxy 的并发读写(握手取凭据 vs admin 动态改)
	credMu sync.Mutex

	clientsMu sync.Mutex
	clients   map[*star.Session]*clientInfo
}

var _ pluginer.Server = (*CtrlProxyServer)(nil)

func NewCtrlProxyServer(serviceName string, listener *ListenerList, hub *CtrlHub) *CtrlProxyServer {
	return &CtrlProxyServer{
		Name: serviceName,
		Net:  listener.Transport,
		Addr: listener.Addr,

		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,

		settings: &listener.Settings,
		hub:      hub,
		clients:  make(map[*star.Session]*clientInfo),
	}
}

// creds 构建密码 -> 允许注册的 clientProxyName 映射("" 表示不限身份)
func (s *CtrlProxyServer) creds() map[string]string {
	s.credMu.Lock()
	defer s.credMu.Unlock()

	creds := make(map[string]string)
	for _, cp := range s.settings.ClientProxy {
		if cp.KeyPassword != "" {
			creds[cp.KeyPassword] = cp.ClientProxyName
		}
	}
	if s.settings.KeyPassword != "" {
		creds[s.settings.KeyPassword] = ""
	}
	return creds
}

// ClientProxies 返回当前配置的客户端凭据/流定义快照
func (s *CtrlProxyServer) ClientProxies() []ClientProxy {
	s.credMu.Lock()
	defer s.credMu.Unlock()

	out := make([]ClientProxy, len(s.settings.ClientProxy))
	copy(out, s.settings.ClientProxy)
	return out
}

// AddClientProxy 动态添加客户端凭据/流定义,重复时返回 false
func (s *CtrlProxyServer) AddClientProxy(cp ClientProxy) bool {
	s.credMu.Lock()
	defer s.credMu.Unlock()

	for _, exist := range s.settings.ClientProxy {
		if exist.ClientProxyName == cp.ClientProxyName && exist.StreamId == cp.StreamId {
			return false
		}
	}
	s.settings.ClientProxy = append(s.settings.ClientProxy, cp)
	return true
}

// RemoveClientProxy 动态删除客户端凭据/流定义,返回是否存在
// 已建立的会话不受影响,重连时新凭据表即刻生效
func (s *CtrlProxyServer) RemoveClientProxy(clientProxyName, streamId string) bool {
	s.credMu.Lock()
	defer s.credMu.Unlock()

	list := s.settings.ClientProxy
	for i, exist := range list {
		if exist.ClientProxyName == clientProxyName && exist.StreamId == streamId {
			s.settings.ClientProxy = append(list[:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// Hub 暴露共享中枢,proxy 监听器等数据面组件使用
func (s *CtrlProxyServer) Hub() *CtrlHub { return s.hub }

// Clients 返回当前在线客户端快照
func (s *CtrlProxyServer) Clients() []ClientInfo {
	s.clientsMu.Lock()
	infos := make([]clientInfo, 0, len(s.clients))
	for _, info := range s.clients {
		infos = append(infos, *info)
	}
	s.clientsMu.Unlock()

	out := make([]ClientInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, ClientInfo{
			Name:     info.Name,
			Identity: info.Identity,
			Remote:   info.Remote,
			Since:    info.Since,
			Streams:  len(s.hub.Streams.FindByClientProxy(info.Identity)),
		})
	}
	return out
}

// Kick 断开某个身份名下的全部客户端会话(其重连逻辑需凭新凭据认证)
func (s *CtrlProxyServer) Kick(clientProxyName string) int {
	s.clientsMu.Lock()
	var targets []*star.Session
	for sess, info := range s.clients {
		if info.Identity == clientProxyName {
			targets = append(targets, sess)
		}
	}
	s.clientsMu.Unlock()

	for _, sess := range targets {
		sess.Close()
	}
	return len(targets)
}

// Listen 当前监听器地址(供查询)
func (s *CtrlProxyServer) Listen() (net.Listener, error) {
	return listenTCP(s.Addr, *s.settings)
}

func (s *CtrlProxyServer) ListenPacket() (net.PacketConn, error) {
	// ctrl 通道只走 TCP
	return nil, nil
}

func (s *CtrlProxyServer) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *CtrlProxyServer) ServePacket(p net.PacketConn) error {
	// ctrl 通道只走 TCP
	return nil
}

func (s *CtrlProxyServer) handleConn(conn net.Conn) {
	sess := star.NewSession(conn)

	// 握手校验并确定客户端身份:密码对应的 clientProxyName,"" 为管理员
	var identity string
	creds := s.creds()
	req, err := star.ServerHandshakeVerify(sess, func(r star.HandshakeReq) bool {
		name, ok := creds[r.KeyPassword]
		if !ok {
			return false
		}
		identity = name
		return true
	})
	if err != nil {
		slog.Warn("ctrl handshake failed", "remote", conn.RemoteAddr(), "err", err)
		conn.Close()
		return
	}
	slog.Info("ctrl client connected", "client", req.ClientName, "identity", identityOrAny(identity), "remote", conn.RemoteAddr())

	info := &clientInfo{
		Name:     req.ClientName,
		Identity: identity,
		Remote:   conn.RemoteAddr().String(),
		Since:    time.Now(),
	}
	s.clientsMu.Lock()
	s.clients[sess] = info
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, sess)
		s.clientsMu.Unlock()

		// 先关连接映射再注销流,清理顺序保证下行投递有处可去
		if n := s.hub.CloseConnsByClient(sess); n > 0 {
			slog.Info("ctrl client closed conns", "client", req.ClientName, "count", n)
		}
		if n := s.hub.Streams.RemoveByClient(sess); n > 0 {
			slog.Info("ctrl client disconnected, removed streams", "client", req.ClientName, "count", n)
		}
		conn.Close()
	}()

	for {
		msg, err := sess.Receive()
		if err != nil {
			slog.Info("ctrl session end", "client", req.ClientName, "err", err)
			return
		}

		switch msg.Type {
		case star.MsgRegisterStream:
			if _, err := star.ServerRegisterStream(sess, msg, func(r star.RegisterStreamReq) (uint32, error) {
				if identity != "" && r.ClientProxyName != identity {
					return 0, fmt.Errorf("clientProxyName %q not allowed for this credential", r.ClientProxyName)
				}
				return s.hub.Streams.Assign(sess, r)
			}); err != nil {
				slog.Warn("register stream", "client", req.ClientName, "err", err)
			}
		case star.MsgConnOpenAck:
			var ack star.ConnOpenAck
			if err := star.DecodeControl(msg, &ack); err != nil {
				slog.Warn("bad connOpenAck", "err", err)
				continue
			}
			s.hub.DeliverConnAck(ack)
		case star.MsgPing:
			if err := sess.Send(&star.Message{Type: star.MsgPong, StreamID: msg.StreamID, Payload: msg.Payload}); err != nil {
				slog.Warn("send pong", "err", err)
			}
		case star.MsgData:
			if !s.hub.Downlink(msg.StreamID, msg.Payload) {
				slog.Debug("data for unknown conn", "conn", msg.StreamID)
			}
		case star.MsgStreamClose:
			// 先按连接处理(客户端侧后端关闭,半关闭语义排空在途数据);
			// 不是连接则视为注销服务流
			if s.hub.ClientClosed(msg.StreamID) {
				slog.Debug("conn closed by client", "conn", msg.StreamID)
			} else {
				s.hub.Streams.Remove(msg.StreamID)
				slog.Debug("stream closed", "stream", msg.StreamID)
			}
		default:
			slog.Debug("unexpected message", "type", msg.TypeName())
		}
	}
}

func identityOrAny(identity string) string {
	if identity == "" {
		return "*"
	}
	return identity
}
