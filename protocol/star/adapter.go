package star

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

// streamFeedCap 单条流连接的投递队列长度(帧数)。
// 队列满时投递方(Session 读循环)阻塞——与既有 writeConn 阻塞语义一致:
// v1 采用会话级背压(已知取舍:跨流头阻塞),按流窗口在 R8 流控任务落地。
const streamFeedCap = 32

// writeChunk StreamConn.Write 的分帧大小(不超过 MaxPayloadSize)
const writeChunk = 64 << 10

// StreamConn 把一条 star 流上的连接级数据通道适配为 rainnet.Conn。
//
// 生命周期语义(与 docs/design.md 半关闭矩阵一致):
//   - CloseWrite:发送半关帧("我不再上行"),读方向继续;
//   - 读到半关帧:本侧后续 Read 返回 EOF(写方向仍可用);
//   - Close / 读到全关帧:整条终止;
//   - Session 断开:所有流连接以 EOF 解除阻塞。
//
// 单一读者约定:Read 只能由一个 goroutine 调用(与 Session.Receive 相同);Write 可并发。
type StreamConn struct {
	sess      *Session
	connID    uint32
	feed      chan feedItem
	closeOnce sync.Once
	closed    chan struct{}
	closedErr error
	readEOF   atomic.Bool
	writeHalf atomic.Bool
	pending   []byte // Read 缓冲小于帧时的剩余部分(仅 Read goroutine 触碰)
}

type feedItem struct {
	data []byte
	half bool
	full bool
}

var _ rainnet.Conn = (*StreamConn)(nil)

func newStreamConn(sess *Session, connID uint32) *StreamConn {
	return &StreamConn{
		sess:   sess,
		connID: connID,
		feed:   make(chan feedItem, streamFeedCap),
		closed: make(chan struct{}),
	}
}

func (sc *StreamConn) Read(p []byte) (int, error) {
	for {
		if sc.readEOF.Load() {
			return 0, io.EOF
		}
		if len(sc.pending) > 0 {
			n := copy(p, sc.pending)
			sc.pending = sc.pending[n:]
			return n, nil
		}
		item, ok := sc.recv()
		if !ok {
			// 本地 Close/会话断开;排空已投递帧后按关闭原因返回
			return 0, sc.closedErr
		}
		if item.full {
			sc.readEOF.Store(true)
			return 0, io.EOF
		}
		if item.half {
			sc.readEOF.Store(true)
			return 0, io.EOF
		}
		if len(item.data) == 0 {
			continue
		}
		n := copy(p, item.data)
		if n < len(item.data) {
			sc.pending = item.data[n:]
		}
		return n, nil
	}
}

// recv 取一条已投递帧;本地关闭时先排空队列再返回 false(保持帧序)。
func (sc *StreamConn) recv() (feedItem, bool) {
	select {
	case it := <-sc.feed:
		return it, true
	case <-sc.closed:
		select {
		case it := <-sc.feed:
			return it, true
		default:
			return feedItem{}, false
		}
	}
}

func (sc *StreamConn) Write(p []byte) (int, error) {
	if sc.isClosed() {
		return 0, net.ErrClosed
	}
	total := len(p)
	for off := 0; off < total; off += writeChunk {
		end := off + writeChunk
		if end > total {
			end = total
		}
		if err := sc.sess.Send(&Message{Type: MsgData, StreamID: sc.connID, Payload: p[off:end]}); err != nil {
			return off, err
		}
	}
	return total, nil
}

// CloseWrite 半关闭:发送半关帧,仅停上行。重复调用为空操作。
func (sc *StreamConn) CloseWrite() error {
	if sc.writeHalf.Swap(true) {
		return nil
	}
	return sc.sess.Send(HalfClose(sc.connID))
}

// Close 整条终止:发送全关帧并解除本地阻塞。
func (sc *StreamConn) Close() error {
	sc.markClosed(net.ErrClosed)
	return sc.sess.Send(&Message{Type: MsgStreamClose, StreamID: sc.connID})
}

// markClosed 解除读阻塞;err 为之后 Read/Write 返回的原因。
func (sc *StreamConn) markClosed(err error) {
	sc.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		sc.closedErr = err
		close(sc.closed)
	})
}

func (sc *StreamConn) isClosed() bool {
	select {
	case <-sc.closed:
		return true
	default:
		return false
	}
}

// LocalAddr/RemoteAddr 以连接 ID 标识流两端(元数据意义大于地址意义)
func (sc *StreamConn) LocalAddr() net.Addr {
	return streamAddr{net: "star", id: sc.connID, local: true}
}
func (sc *StreamConn) RemoteAddr() net.Addr { return streamAddr{net: "star", id: sc.connID} }

// Meta 流连接的元数据(目标由 connOpen 语义决定,不经 SetTarget 改写)
func (sc *StreamConn) Meta() rainnet.Meta {
	return rainnet.Meta{ID: fmt.Sprintf("star-conn-%d", sc.connID), Remote: sc.RemoteAddr().String()}
}

func (sc *StreamConn) SetTarget(string) {}

// SetDeadline 同时作用于读写(经 Session 层的连接 deadline,流级粒度 v1 不做)
func (sc *StreamConn) SetDeadline(t time.Time) error      { return sc.sess.Conn().SetDeadline(t) }
func (sc *StreamConn) SetReadDeadline(t time.Time) error  { return sc.sess.Conn().SetReadDeadline(t) }
func (sc *StreamConn) SetWriteDeadline(t time.Time) error { return sc.sess.Conn().SetWriteDeadline(t) }

type streamAddr struct {
	net   string
	id    uint32
	local bool
}

func (a streamAddr) Network() string { return a.net }
func (a streamAddr) String() string {
	if a.local {
		return fmt.Sprintf("star:local-%d", a.id)
	}
	return fmt.Sprintf("star:conn-%d", a.id)
}

// StreamHub 管理同一条 Session 上的多条 StreamConn。
// Session 读循环的持有方把数据/关流帧经 Feed 投进来,把 ConnOpenAck 也交给 Feed;
// 拨号侧经 OpenStream 建立到内网的连接。
type StreamHub struct {
	sess *Session

	mu      sync.Mutex
	conns   map[uint32]*StreamConn
	acks    map[uint32]chan ConnOpenAck
	connSeq uint32

	onRemove func(connID uint32) // 连接移除回调(如服务端清理流表)
	dead     atomic.Bool
}

func NewStreamHub(sess *Session) *StreamHub {
	return &StreamHub{
		sess:  sess,
		conns: make(map[uint32]*StreamConn),
		acks:  make(map[uint32]chan ConnOpenAck),
	}
}

// OnRemove 设置连接移除回调(连接被全关帧或 Close 移除时触发一次)
func (h *StreamHub) OnRemove(fn func(connID uint32)) { h.onRemove = fn }

// NewStreamConn 在本侧登记并新建一条流连接(接受 ConnOpen 的一侧使用);
// 会话已断开时返回 nil。
func (h *StreamHub) NewStreamConn(connID uint32) *StreamConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dead.Load() {
		return nil
	}
	sc := newStreamConn(h.sess, connID)
	h.conns[connID] = sc
	return sc
}

// OpenStream 请求对端建立一条连接(发送 ConnOpen 并等待应答)。
func (h *StreamHub) OpenStream(ctx context.Context, streamID uint32, timeout time.Duration) (*StreamConn, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	h.mu.Lock()
	if h.dead.Load() {
		h.mu.Unlock()
		return nil, errors.New("star: hub session closed")
	}
	h.connSeq++
	connID := h.connSeq
	ackCh := make(chan ConnOpenAck, 1)
	h.acks[connID] = ackCh
	sc := newStreamConn(h.sess, connID)
	h.conns[connID] = sc
	h.mu.Unlock()

	if err := h.sess.SendControl(MsgConnOpen, ConnOpenReq{StreamID: streamID, ConnID: connID}); err != nil {
		h.remove(connID)
		return nil, fmt.Errorf("star: open stream conn %d: %w", connID, err)
	}

	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case ack := <-ackCh:
		if ack.OK {
			return sc, nil
		}
		h.remove(connID)
		return nil, fmt.Errorf("star: open stream conn %d rejected: %s", connID, ack.Message)
	case <-ctx2.Done():
		h.remove(connID)
		return nil, fmt.Errorf("star: open stream conn %d: %w", connID, ctx2.Err())
	}
}

// Feed 由 Session 读循环调用:投递一帧。返回 true 表示已被 hub 消费
// (data/streamClose/connOpenAck),false 表示与流无关,读循环自行处理。
func (h *StreamHub) Feed(msg *Message) bool {
	switch msg.Type {
	case MsgData:
		if sc := h.get(msg.StreamID); sc != nil {
			sc.deliver(feedItem{data: msg.Payload})
			return true
		}
	case MsgStreamClose:
		if sc := h.get(msg.StreamID); sc != nil {
			h.remove(msg.StreamID)
			sc.deliver(feedItem{half: msg.IsHalfClose(), full: !msg.IsHalfClose()})
			return true
		}
	case MsgConnOpenAck:
		var ack ConnOpenAck
		if err := DecodeControl(msg, &ack); err != nil {
			slog.Warn("star hub: bad connOpenAck", "err", err)
			return true
		}
		h.mu.Lock()
		ch, ok := h.acks[ack.ConnID]
		delete(h.acks, ack.ConnID)
		h.mu.Unlock()
		if ok {
			ch <- ack
			return true
		}
	}
	return false
}

// SessionClosed 读循环退出时调用:所有流连接以 EOF 解除阻塞,后续 OpenStream 失败。
func (h *StreamHub) SessionClosed(err error) {
	h.mu.Lock()
	if h.dead.Swap(true) {
		h.mu.Unlock()
		return
	}
	conns := h.conns
	acks := h.acks
	h.conns = make(map[uint32]*StreamConn)
	h.acks = make(map[uint32]chan ConnOpenAck)
	h.mu.Unlock()

	for _, sc := range conns {
		sc.markClosed(io.EOF) // 会话断开:按 EOF 收尾(在途帧仍可排空)
	}
	for _, ch := range acks {
		ch <- ConnOpenAck{Message: "session closed"}
	}
}

// Remove 主动移除一条流连接(不发送任何帧)。
func (h *StreamHub) Remove(connID uint32) {
	if sc := h.get(connID); sc != nil {
		h.remove(connID)
		sc.markClosed(net.ErrClosed)
	}
}

func (h *StreamHub) get(connID uint32) *StreamConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[connID]
}

func (h *StreamHub) remove(connID uint32) {
	h.mu.Lock()
	_, ok := h.conns[connID]
	delete(h.conns, connID)
	delete(h.acks, connID)
	fn := h.onRemove
	h.mu.Unlock()
	if ok && fn != nil {
		fn(connID)
	}
}

// deliver 投递一帧;队列满时阻塞(会话级背压,见 streamFeedCap),本地已关闭则丢弃。
func (sc *StreamConn) deliver(item feedItem) {
	select {
	case sc.feed <- item:
	case <-sc.closed:
	}
}
