package starserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"rain-net/protocol/star"
)

// openConnTimeout 建立连接请求的应答等待超时
const openConnTimeout = 10 * time.Second

// downlinkQueue 每条连接的下行队列长度(条数),积满说明外部连接消费过慢,
// 会阻塞 ctrl 读循环(头阻塞),当前作为已知取舍
const downlinkQueue = 256

// ConnEntry 一条外部连接在 ctrl 通道上的映射
type ConnEntry struct {
	ConnID   uint32
	StreamID uint32 // 所属服务流的数字ID

	ClientProxyName string
	StreamId        string

	// Client 承载该连接的 ctrl 会话
	Client *star.Session
	// stream 所属流(流量统计挂在这里),可为 nil
	stream *StreamEntry

	OpenedAt time.Time

	// sink 下行数据队列(ctrl 读循环 -> 外部连接泵)
	sink chan []byte
	// done 关闭信号,关闭后下行泵退出且不再接受投递
	done chan struct{}
	// remoteClosed 客户端侧后端已关闭:下行泵排空在途数据后收尾
	remoteClosed chan struct{}

	once sync.Once
	rcMb sync.Once
}

func (e *ConnEntry) close() {
	e.once.Do(func() { close(e.done) })
}

func (e *ConnEntry) markRemoteClosed() {
	e.rcMb.Do(func() { close(e.remoteClosed) })
}

// CtrlHub ctrl 通道的共享中枢:同一个 starContext 下的 ctrlproxy 与
// proxy 监听器通过它衔接——ctrlproxy 往里写流表和客户端上行数据,
// proxy 监听器从这里开连接、取下行数据
type CtrlHub struct {
	Streams *StreamTable

	mu      sync.Mutex
	conns   map[uint32]*ConnEntry
	acks    map[uint32]chan star.ConnOpenAck
	connSeq uint32
}

func NewCtrlHub() *CtrlHub {
	return &CtrlHub{
		Streams: NewStreamTable(),
		conns:   make(map[uint32]*ConnEntry),
		acks:    make(map[uint32]chan star.ConnOpenAck),
	}
}

// OpenConn 通过某条已注册流,请求客户端建立一条到内网服务的连接
// 发送 MsgConnOpen 并阻塞等待应答
func (h *CtrlHub) OpenConn(entry *StreamEntry) (*ConnEntry, error) {
	h.mu.Lock()
	h.connSeq++
	connID := h.connSeq
	ackCh := make(chan star.ConnOpenAck, 1)
	h.acks[connID] = ackCh

	// 先登记连接映射再发请求,避免客户端上行先于映射到达被丢弃
	e := &ConnEntry{
		ConnID:          connID,
		StreamID:        entry.AssignID,
		ClientProxyName: entry.ClientProxyName,
		StreamId:        entry.StreamId,
		Client:          entry.Client,
		stream:          entry,
		OpenedAt:        time.Now(),
		sink:            make(chan []byte, downlinkQueue),
		done:            make(chan struct{}),
		remoteClosed:    make(chan struct{}),
	}
	h.conns[connID] = e
	h.mu.Unlock()

	req := star.ConnOpenReq{StreamID: entry.AssignID, ConnID: connID}
	payload, err := json.Marshal(req)
	if err == nil {
		err = entry.Client.Send(&star.Message{
			Type:     star.MsgConnOpen,
			StreamID: entry.AssignID,
			Payload:  payload,
		})
	}
	if err == nil {
		select {
		case ack := <-ackCh:
			if ack.OK {
				return e, nil
			}
			err = fmt.Errorf("star: open conn %d rejected: %s", connID, ack.Message)
		case <-time.After(openConnTimeout):
			err = fmt.Errorf("star: open conn %d timeout", connID)
		}
	}

	// 失败收尾
	h.removeConn(connID)
	e.close()
	return nil, err
}

// Downlink ctrl 读循环投递客户端上行数据,false 表示连接不存在
// 队列满时阻塞等待,保证数据完整有序(已知取舍:会头阻塞)
func (h *CtrlHub) Downlink(connID uint32, payload []byte) bool {
	h.mu.Lock()
	e := h.conns[connID]
	h.mu.Unlock()
	if e == nil {
		return false
	}
	if e.stream != nil {
		e.stream.OutBytes.Add(uint64(len(payload)))
	}

	select {
	case e.sink <- payload:
		return true
	case <-e.done:
		return false
	}
}

// DeliverConnAck ctrl 读循环投递连接建立应答
func (h *CtrlHub) DeliverConnAck(ack star.ConnOpenAck) {
	h.mu.Lock()
	ch := h.acks[ack.ConnID]
	delete(h.acks, ack.ConnID)
	h.mu.Unlock()

	if ch != nil {
		ch <- ack
	}
}

// ClientClosed 客户端侧后端连接已关闭(客户端发来关流帧)
//
// 语义为半关闭:ctrl 读循环按序处理,调用时所有在途下行数据都已进入
// sink,下行泵排空后自行收尾;映射随之移除。返回是否存在该连接
func (h *CtrlHub) ClientClosed(connID uint32) bool {
	h.mu.Lock()
	e := h.conns[connID]
	delete(h.conns, connID)
	h.mu.Unlock()

	if e == nil {
		return false
	}
	e.markRemoteClosed()
	return true
}

// CloseConn 关闭一条连接映射,notify 为 true 时向客户端发送关流帧
// 返回是否存在该连接
func (h *CtrlHub) CloseConn(connID uint32, notify bool) bool {
	h.mu.Lock()
	e := h.conns[connID]
	delete(h.conns, connID)
	h.mu.Unlock()

	if e == nil {
		return false
	}
	e.close()
	if notify {
		_ = e.Client.Send(&star.Message{Type: star.MsgStreamClose, StreamID: connID})
	}
	return true
}

// HasConn 查询连接是否存在
func (h *CtrlHub) HasConn(connID uint32) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[connID]
	return ok
}

// CloseConnsByClient 客户端会话断开时,关闭其承载的所有连接
func (h *CtrlHub) CloseConnsByClient(sess *star.Session) int {
	h.mu.Lock()
	var closing []*ConnEntry
	for id, e := range h.conns {
		if entry, ok := h.Streams.Get(e.StreamID); ok && entry.Client == sess {
			closing = append(closing, e)
			delete(h.conns, id)
		}
	}
	h.mu.Unlock()

	for _, e := range closing {
		e.close()
	}
	return len(closing)
}

// ConnCount 当前连接数
func (h *CtrlHub) ConnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// ConnStat 连接的只读快照,用于 API 查询
type ConnStat struct {
	ConnID          uint32    `json:"connId"`
	StreamAssignID  uint32    `json:"streamAssignId"`
	ClientProxyName string    `json:"clientProxyName"`
	StreamId        string    `json:"streamId"`
	OpenedAt        time.Time `json:"openedAt"`
}

// SnapshotConns 返回当前全部连接的快照,按连接ID排列
func (h *CtrlHub) SnapshotConns() []ConnStat {
	h.mu.Lock()
	defer h.mu.Unlock()

	stats := make([]ConnStat, 0, len(h.conns))
	for _, e := range h.conns {
		stats = append(stats, ConnStat{
			ConnID:          e.ConnID,
			StreamAssignID:  e.StreamID,
			ClientProxyName: e.ClientProxyName,
			StreamId:        e.StreamId,
			OpenedAt:        e.OpenedAt,
		})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].ConnID < stats[j].ConnID })
	return stats
}

func (h *CtrlHub) removeConn(connID uint32) {
	h.mu.Lock()
	e := h.conns[connID]
	delete(h.conns, connID)
	h.mu.Unlock()
	if e != nil {
		e.close()
	}
}
