// Package dns 提供 DNS 服务处理器(design.md §七 的旗舰示例):
//   - 数据面:zone 表精确匹配应答 A 记录;未命中转发上游(可配多个,轮询);
//     TCP-DNS 帧循环(RFC 1035 §4.2.2 两字节长度前缀),经 transport 注册表挂载;
//   - 控制面:实现 pluginer.APIProvider,经 admin 聚合暴露 /api/dns/{name}/zones,
//     zone 变更即时生效并可选持久化到 configstore(§七 组件自治)。
//
// v1 边界:仅 A 记录应答 + UDP 上游转发;入站建议 tcp 传输(UDP 数据报为
// PacketConn 维度,transport 层 v1 不建模,见 roadmap)。
package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/haisentree/rain-net/internal/configstore"
	"github.com/haisentree/rain-net/pluginer"
)

// Handler DNS 服务组件。
type Handler struct {
	name      string
	mu        sync.RWMutex
	zones     map[string]map[string]string // zone(如 "example.org") → 主机名(相对 zone)→ A 记录 IP
	upstreams []string                     // 上游 DNS(未命中 zone 时转发,轮询);空则直接 NXDOMAIN
	rrIdx     atomic.Uint64
	store     *configstore.FileStore // 可选:动态变更持久化(nil = 仅内存)
	storeKey  string
	injected  net.Listener // 外部注入的监听器(嗅探分发),非 nil 时优先
}

// New 创建 DNS 处理器。zones: zone → (主机名 → IP)。
func New(name string, zones map[string]map[string]string, upstreams []string) *Handler {
	return &Handler{name: name, zones: normalizeZones(zones), upstreams: upstreams}
}

// UseStore 挂载持久化后端(PUT zone 时写入,启动时可回放)。
func (h *Handler) UseStore(store *configstore.FileStore, key string) *Handler {
	h.store = store
	h.storeKey = key
	return h
}

// Zones 返回 zone 表快照。
func (h *Handler) Zones() map[string]map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]map[string]string, len(h.zones))
	for zone, hosts := range h.zones {
		out[zone] = hosts
	}
	return out
}

func normalizeZones(zones map[string]map[string]string) map[string]map[string]string {
	nz := make(map[string]map[string]string, len(zones))
	for zone, hosts := range zones {
		inner := make(map[string]string, len(hosts))
		for hostname, ip := range hosts {
			inner[strings.ToLower(strings.TrimSuffix(strings.ToLower(hostname), "."))] = ip
		}
		nz[strings.ToLower(strings.TrimSuffix(strings.ToLower(zone), "."))] = inner
	}
	return nz
}

var (
	_ pluginer.Server      = (*Handler)(nil)
	_ pluginer.APIProvider = (*Handler)(nil)
)

// RegisterAPI 实现 APIProvider:向 admin 控制面注册 zone 管理 API(§七 组件自治)。
func (h *Handler) RegisterAPI(mux *http.ServeMux) {
	prefix := "/api/dns/" + h.name + "/zones"
	println("DNS RegisterAPI:", prefix)
	mux.HandleFunc("GET "+prefix, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.Zones())
	})
	mux.HandleFunc("PUT "+prefix, func(w http.ResponseWriter, r *http.Request) {
		var zones map[string]map[string]string
		if err := json.NewDecoder(r.Body).Decode(&zones); err != nil {
			http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
			return
		}
		h.replaceAll(zones)
		if h.store != nil {
			data, _ := json.Marshal(zones)
			_ = h.store.Put(context.Background(), h.storeKey, data) // 持久化失败不阻断运行时生效
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (h *Handler) replaceAll(zones map[string]map[string]string) {
	h.mu.Lock()
	h.zones = normalizeZones(zones)
	h.mu.Unlock()
}

// Listen 装配层注入监听器(UseListener)或调用方自行管理时返回 nil 占位。
func (h *Handler) Listen() (net.Listener, error) {
	if h.injected != nil {
		return h.injected, nil
	}
	return nil, nil // UDP 数据报维度 v1 不建模;DNS over TCP 由 Serve 承载
}

// UseListener 注入外部监听器(嗅探分发等装配场景)。
func (h *Handler) UseListener(ln net.Listener) *Handler {
	h.injected = ln
	return h
}

// Serve TCP-DNS 服务循环:每连接按 2 字节长度前缀分帧(RFC 1035 §4.2.2)。
func (h *Handler) Serve(l net.Listener) error {
	if l == nil {
		return nil
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go h.serveConn(conn)
	}
}

// ServePacket UDP 数据报维度 v1 不建模(roadmap);DNS over UDP 上游转发不受影响
func (h *Handler) ServePacket(p net.PacketConn) error {
	return nil
}

// ListenPacket UDP 数据报维度 v1 不建模(roadmap)。
func (h *Handler) ListenPacket() (net.PacketConn, error) {
	return nil, nil
}

func (h *Handler) serveConn(conn net.Conn) {
	defer conn.Close()
	for {
		var lenBuf [2]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		msgLen := int(lenBuf[0])<<8 | int(lenBuf[1])
		if msgLen <= 0 || msgLen > 64<<10 {
			return
		}
		buf := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		q := new(dns.Msg)
		if err := q.Unpack(buf); err != nil {
			return
		}
		packed, err := h.answer(q).Pack()
		if err != nil {
			return
		}
		out := make([]byte, 2+len(packed))
		out[0] = byte(len(packed) >> 8)
		out[1] = byte(len(packed))
		copy(out[2:], packed)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

// answer 应答逻辑(zone 精确匹配 A 记录;未命中且有上游则转发,轮询)。
func (h *Handler) answer(q *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(q)
	resp.RecursionAvailable = true
	for _, question := range q.Question {
		if ip, ok := h.lookup(question.Name); ok {
			if rr, err := dns.NewRR(fmt.Sprintf("%s 60 IN A %s", question.Name, ip)); err == nil {
				resp.Answer = append(resp.Answer, rr)
			}
		}
	}
	if len(resp.Answer) == 0 && len(q.Question) > 0 && len(h.upstreams) > 0 {
		idx := h.rrIdx.Add(1)
		upstream := h.upstreams[idx%uint64(len(h.upstreams))]
		client := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
		if upstreamResp, _, err := client.Exchange(q, upstream); err == nil {
			return upstreamResp
		}
		resp.SetRcode(q, dns.RcodeServerFailure)
	}
	return resp
}

// lookup zone 后缀精确匹配:查询名以 zone 结尾,主机部分在表内查找。
func (h *Handler) lookup(qname string) (string, bool) {
	name := strings.ToLower(strings.TrimSuffix(qname, "."))
	h.mu.RLock()
	defer h.mu.RUnlock()
	for zone, hosts := range h.zones {
		if name == zone {
			continue // 裸 zone 无 A 记录语义
		}
		if strings.HasSuffix(name, "."+zone) {
			host := strings.TrimSuffix(name, "."+zone)
			if ip, ok := hosts[host]; ok {
				return ip, true
			}
		}
	}
	return "", false
}

func (h *Handler) Name() string { return "dns:" + h.name }
