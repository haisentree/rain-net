package starserver

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rain-net/pluginer"
)

// AdminServer 管理 API 监听器(type: admin)
//
// 提供运行时查询与配置能力,鉴权为 Bearer token(settings.adminPassword):
//
//	GET    /api/status                                              总览
//	GET    /api/clients                                             在线客户端
//	GET    /api/streams                                             已注册流(含流量)
//	GET    /api/conns                                               活跃连接
//	GET    /api/connects                                            路由规则
//	POST   /api/clientProxies                                       动态添加客户端凭据/流定义
//	DELETE /api/clientProxies/{clientProxyName}/{streamId}          删除(?kick=1 同时踢下线)
//	POST   /api/connects                                            动态添加路由规则
//	DELETE /api/connects/{proxyName}/{clientProxyName}/{streamId}   删除路由规则
//	POST   /api/kick                                                踢指定身份的客户端下线
//
// 注意:动态修改只作用于运行时,进程重启后以配置文件为准(持久化后续接入配置中心)
type AdminServer struct {
	Name string
	Net  string
	Addr string

	settings *Settings

	hub      *CtrlHub
	connects *ConnectTable
	ctrls    []*CtrlProxyServer

	started time.Time
}

var _ pluginer.Server = (*AdminServer)(nil)

func NewAdminServer(name string, listener *ListenerList, hub *CtrlHub, connects *ConnectTable, ctrls []*CtrlProxyServer) *AdminServer {
	return &AdminServer{
		Name: name,
		Net:  listener.Transport,
		Addr: listener.Addr,

		settings: &listener.Settings,
		hub:      hub,
		connects: connects,
		ctrls:    ctrls,
		started:  time.Now(),
	}
}

func (s *AdminServer) Listen() (net.Listener, error) {
	return listenTCP(s.Addr, *s.settings)
}

func (s *AdminServer) ListenPacket() (net.PacketConn, error) {
	return nil, nil
}

func (s *AdminServer) Serve(l net.Listener) error {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/status", s.handleStatus)
	apiMux.HandleFunc("GET /api/clients", s.handleClients)
	apiMux.HandleFunc("GET /api/streams", s.handleStreams)
	apiMux.HandleFunc("GET /api/conns", s.handleConns)
	apiMux.HandleFunc("GET /api/connects", s.handleConnects)
	apiMux.HandleFunc("GET /api/clientProxies", s.handleListClientProxies)
	apiMux.HandleFunc("POST /api/clientProxies", s.handleAddClientProxy)
	apiMux.HandleFunc("DELETE /api/clientProxies/{clientProxyName}/{streamId}", s.handleDeleteClientProxy)
	apiMux.HandleFunc("POST /api/connects", s.handleAddConnect)
	apiMux.HandleFunc("DELETE /api/connects/{proxyName}/{clientProxyName}/{streamId}", s.handleDeleteConnect)
	apiMux.HandleFunc("POST /api/kick", s.handleKick)

	// 内嵌 Web 管理台:静态资源公开(页面无敏感数据),
	// /api/* 统一走 Bearer 鉴权
	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	webMux := http.NewServeMux()
	webMux.Handle("/", http.FileServer(http.FS(webRoot)))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.auth(apiMux).ServeHTTP(w, r)
			return
		}
		webMux.ServeHTTP(w, r)
	})

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	return srv.Serve(l)
}

func (s *AdminServer) ServePacket(p net.PacketConn) error {
	return nil
}

// auth Bearer token 鉴权;未配置 adminPassword 时拒绝所有请求(安全默认)
func (s *AdminServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.settings.AdminPassword == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin password not configured"})
			return
		}
		const prefix = "Bearer "
		token := r.Header.Get("Authorization")
		good := len(token) > len(prefix) && token[:len(prefix)] == prefix &&
			subtleComp(token[len(prefix):], s.settings.AdminPassword)
		if !good {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// subtleComp 恒定时间字符串比较,避免时序侧信道
func subtleComp(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *AdminServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	streams := s.hub.Streams.Snapshot()
	var in, out uint64
	for _, st := range streams {
		in += st.InBytes
		out += st.OutBytes
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     s.Name,
		"uptime":   int64(time.Since(s.started) / time.Second),
		"clients":  s.countClients(),
		"streams":  len(streams),
		"conns":    s.hub.ConnCount(),
		"inBytes":  in,
		"outBytes": out,
	})
}

func (s *AdminServer) countClients() int {
	n := 0
	for _, c := range s.ctrls {
		n += len(c.Clients())
	}
	return n
}

func (s *AdminServer) handleClients(w http.ResponseWriter, r *http.Request) {
	var clients []ClientInfo
	for _, c := range s.ctrls {
		clients = append(clients, c.Clients()...)
	}
	if clients == nil {
		clients = []ClientInfo{}
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *AdminServer) handleStreams(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.hub.Streams.Snapshot())
}

func (s *AdminServer) handleConns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.hub.SnapshotConns())
}

func (s *AdminServer) handleConnects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.connects.All())
}

// handleListClientProxies 返回配置中的客户端凭据/流定义(跨 ctrlproxy 去重)
func (s *AdminServer) handleListClientProxies(w http.ResponseWriter, r *http.Request) {
	seen := make(map[string]bool)
	out := make([]ClientProxy, 0)
	for _, c := range s.ctrls {
		for _, cp := range c.ClientProxies() {
			key := cp.ClientProxyName + "/" + cp.StreamId
			if !seen[key] {
				seen[key] = true
				out = append(out, cp)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddClientProxy 动态添加客户端凭据/流定义
func (s *AdminServer) handleAddClientProxy(w http.ResponseWriter, r *http.Request) {
	var cp ClientProxy
	if err := json.NewDecoder(r.Body).Decode(&cp); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if cp.ClientProxyName == "" || cp.StreamId == "" || cp.KeyPassword == "" {
		writeErr(w, http.StatusBadRequest, "clientProxyName, streamId, keyPassword are required")
		return
	}

	added := false
	for _, c := range s.ctrls {
		if c.AddClientProxy(cp) {
			added = true
		}
	}
	if !added {
		writeErr(w, http.StatusConflict, "clientProxy already exists")
		return
	}
	slog.Info("admin: clientProxy added", "clientProxyName", cp.ClientProxyName, "streamId", cp.StreamId)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "added"})
}

func (s *AdminServer) handleDeleteClientProxy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("clientProxyName")
	streamId := r.PathValue("streamId")

	removed := false
	for _, c := range s.ctrls {
		if c.RemoveClientProxy(name, streamId) {
			removed = true
		}
	}
	if !removed {
		writeErr(w, http.StatusNotFound, "clientProxy not found")
		return
	}

	resp := map[string]string{"ok": "removed"}
	if r.URL.Query().Get("kick") == "1" {
		resp["kicked"] = strconv.Itoa(s.kick(name))
	}
	slog.Info("admin: clientProxy removed", "clientProxyName", name, "streamId", streamId)
	writeJSON(w, http.StatusOK, resp)
}

func (s *AdminServer) handleAddConnect(w http.ResponseWriter, r *http.Request) {
	var rule ConnectItem
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if rule.ProxyName == "" || rule.ClientProxyName == "" || rule.StreamId == "" {
		writeErr(w, http.StatusBadRequest, "proxyName, clientProxyName, streamId are required")
		return
	}
	if !s.connects.Add(rule) {
		writeErr(w, http.StatusConflict, "connect rule already exists")
		return
	}
	slog.Info("admin: connect rule added", "proxyName", rule.ProxyName,
		"clientProxyName", rule.ClientProxyName, "streamId", rule.StreamId)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "added"})
}

func (s *AdminServer) handleDeleteConnect(w http.ResponseWriter, r *http.Request) {
	proxyName := r.PathValue("proxyName")
	name := r.PathValue("clientProxyName")
	streamId := r.PathValue("streamId")

	if !s.connects.Remove(proxyName, name, streamId) {
		writeErr(w, http.StatusNotFound, "connect rule not found")
		return
	}
	slog.Info("admin: connect rule removed", "proxyName", proxyName,
		"clientProxyName", name, "streamId", streamId)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "removed"})
}

// handleKick 踢指定身份的客户端下线
func (s *AdminServer) handleKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientProxyName string `json:"clientProxyName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientProxyName == "" {
		writeErr(w, http.StatusBadRequest, "clientProxyName is required")
		return
	}
	n := s.kick(req.ClientProxyName)
	slog.Info("admin: kicked clients", "clientProxyName", req.ClientProxyName, "count", n)
	writeJSON(w, http.StatusOK, map[string]int{"kicked": n})
}

func (s *AdminServer) kick(clientProxyName string) int {
	n := 0
	for _, c := range s.ctrls {
		n += c.Kick(clientProxyName)
	}
	return n
}
