// Package rainnet 定义组件之间的通用语言:统一连接抽象 Conn 与元数据 Meta。
//
// 设计约定(docs/design.md §三):
//   - Conn 形状永不偏离 net.Conn + 三个附加方法,生态资产(http.Server 等)可直接挂载;
//   - CloseWrite 是半关闭契约的编译期表达,不支持者返回 ErrNotSupported;
//   - Meta 只读,目标经 SetTarget 仅允许写入一次(协议 handler 解析出目标后调用)。
package rainnet

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
)

// ErrNotSupported 表示当前底层连接不支持该操作(如半关闭)。
var ErrNotSupported = errors.New("rainnet: operation not supported on this connection")

// Meta 是随连接携带的只读元数据。
//
// 字段按填充方划分:Listener/Remote 由 ingress 填,Identity 由协议 handler 认证后填,
// Target 由协议 handler 解析后经 SetTarget 写入。
type Meta struct {
	ID       string // 连接 ID,进程内唯一,用于日志贯穿 ingress→handler→router→dialer→bridge
	Listener string // 进入的 ingress 名
	Remote   string // 真实源地址
	Identity string // 协议层认证出的身份(如 star 的 clientProxyName)
	Target   string // 协议层解析出的目标
}

// Conn 是所有组件交换连接的统一形状。
type Conn interface {
	net.Conn
	// CloseWrite 半关闭:仅关闭写方向,读方向继续。
	// 底层不支持时返回 ErrNotSupported。
	CloseWrite() error
	// Meta 返回连接元数据(返回副本,调用方可安全读取)。
	Meta() Meta
	// SetTarget 写入协议层解析出的目标,仅首次调用生效,重复调用为空操作。
	SetTarget(target string)
}

var connSeq atomic.Uint64

// Wrap 把任意 net.Conn 适配为 rainnet.Conn。
// meta 中未填的 ID 与 Remote 会自动补齐。
func Wrap(c net.Conn, meta Meta) Conn {
	if meta.ID == "" {
		meta.ID = fmt.Sprintf("conn-%06d", connSeq.Add(1))
	}
	if meta.Remote == "" && c.RemoteAddr() != nil {
		meta.Remote = c.RemoteAddr().String()
	}
	return &wrapped{Conn: c, meta: meta}
}

type wrapped struct {
	net.Conn
	meta      Meta
	targetSet bool
}

// CloseWrite 半关闭:透传底层同名方法;底层不支持时返回 ErrNotSupported。
// *net.TCPConn 与 crypto/tls.Conn 均原生支持。
func (w *wrapped) CloseWrite() error {
	if cw, ok := w.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return ErrNotSupported
}

func (w *wrapped) Meta() Meta { return w.meta }

func (w *wrapped) SetTarget(target string) {
	if w.targetSet {
		return
	}
	w.targetSet = true
	w.meta.Target = target
}
