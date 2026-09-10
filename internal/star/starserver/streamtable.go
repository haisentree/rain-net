package starserver

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"rain-net/protocol/star"
)

// StreamEntry 一条已注册的流:客户端把内网服务注册到服务端后的记录
type StreamEntry struct {
	AssignID uint32 // 服务端分配的数字流ID
	// 注册时客户端上报的标识,与配置文件中的 clientProxyName/streamId 对应
	ClientProxyName string
	StreamId        string

	// Client 承载该流的 ctrl 会话,数据面收发都走它
	Client *star.Session

	Since time.Time // 注册时间

	// 流量统计(外部用户视角):InBytes 用户->内网,OutBytes 内网->用户
	InBytes  atomic.Uint64
	OutBytes atomic.Uint64
}

// StreamStat 流的只读快照,用于 API 查询
type StreamStat struct {
	AssignID        uint32 `json:"assignId"`
	ClientProxyName string `json:"clientProxyName"`
	StreamId        string `json:"streamId"`
	Since           time.Time `json:"since"`
	InBytes         uint64    `json:"inBytes"`
	OutBytes        uint64    `json:"outBytes"`
}

// key 用于流唯一性校验:clientProxyName/streamId
func streamKey(clientProxyName, streamId string) string {
	return clientProxyName + "/" + streamId
}

// StreamTable 服务端的流表:分配数字流ID并维护注册信息
// 后续 proxy 监听器/bridger 通过它查找某条流落在哪个客户端会话上
type StreamTable struct {
	mu  sync.RWMutex
	seq uint32

	byID       map[uint32]*StreamEntry
	byStreamID map[string]*StreamEntry
}

func NewStreamTable() *StreamTable {
	return &StreamTable{
		byID:       make(map[uint32]*StreamEntry),
		byStreamID: make(map[string]*StreamEntry),
	}
}

// Assign 为一条注册请求分配数字流ID并登记
// 签名与 star.AssignStreamFunc 匹配,可直接传给 star.ServerRegisterStream
func (t *StreamTable) Assign(client *star.Session, req star.RegisterStreamReq) (uint32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := streamKey(req.ClientProxyName, req.StreamId)
	if _, dup := t.byStreamID[key]; dup {
		return 0, fmt.Errorf("stream %s already registered", key)
	}

	t.seq++
	entry := &StreamEntry{
		AssignID:        t.seq,
		ClientProxyName: req.ClientProxyName,
		StreamId:        req.StreamId,
		Client:          client,
		Since:           time.Now(),
	}
	t.byID[entry.AssignID] = entry
	t.byStreamID[key] = entry
	return entry.AssignID, nil
}

// Get 按数字流ID查找
func (t *StreamTable) Get(assignID uint32) (*StreamEntry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.byID[assignID]
	return e, ok
}

// GetByName 按客户端代理名 + 字符串流标识查找
func (t *StreamTable) GetByName(clientProxyName, streamId string) (*StreamEntry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.byStreamID[streamKey(clientProxyName, streamId)]
	return e, ok
}

// FindByClientProxy 按客户端代理名查找其名下所有流,按分配顺序返回
func (t *StreamTable) FindByClientProxy(clientProxyName string) []*StreamEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var entries []*StreamEntry
	for _, e := range t.byID {
		if e.ClientProxyName == clientProxyName {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].AssignID < entries[j].AssignID
	})
	return entries
}

// Remove 按数字流ID注销
func (t *StreamTable) Remove(assignID uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if e, ok := t.byID[assignID]; ok {
		delete(t.byID, assignID)
		delete(t.byStreamID, streamKey(e.ClientProxyName, e.StreamId))
	}
}

// RemoveByClient 注销某个 ctrl 会话名下的所有流,会话断开时调用,返回注销数量
func (t *StreamTable) RemoveByClient(client *star.Session) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	removed := 0
	for id, e := range t.byID {
		if e.Client == client {
			delete(t.byID, id)
			delete(t.byStreamID, streamKey(e.ClientProxyName, e.StreamId))
			removed++
		}
	}
	return removed
}

// Len 当前流数量
func (t *StreamTable) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.byID)
}

// Snapshot 返回全部流的只读快照,按分配顺序排列
func (t *StreamTable) Snapshot() []StreamStat {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := make([]StreamStat, 0, len(t.byID))
	for _, e := range t.byID {
		stats = append(stats, StreamStat{
			AssignID:        e.AssignID,
			ClientProxyName: e.ClientProxyName,
			StreamId:        e.StreamId,
			Since:           e.Since,
			InBytes:         e.InBytes.Load(),
			OutBytes:        e.OutBytes.Load(),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].AssignID < stats[j].AssignID
	})
	return stats
}
