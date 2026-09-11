package star

import (
	"context"
	"errors"
	"sync"
)

// DefaultStreamWindow 每连接发送窗口(字节)
const DefaultStreamWindow = 256 << 10

// Window 每连接发送窗口:发送前按字节数记账,接收方以 WindowUpdate 帧归还额度。
// 零值不可用,经 NewWindow 创建;并发安全。
type Window struct {
	mu          sync.Mutex
	outstanding int64
	limit       int64
	closed      bool
	notify      []chan struct{}
}

func NewWindow(limit int64) *Window {
	return &Window{limit: limit}
}

// ErrWindowClosed 窗口已关闭(连接拆除)
var ErrWindowClosed = errors.New("star: window closed")

// Close 关闭窗口:阻塞中的 Acquire 以 ErrWindowClosed 失败,后续 Acquire 立即失败。
func (w *Window) Close() {
	w.mu.Lock()
	w.closed = true
	waiters := w.notify
	w.notify = nil
	w.mu.Unlock()
	for _, ch := range waiters {
		ch <- struct{}{}
	}
}

// Acquire 占用 n 字节额度;不足时阻塞直到归还或 ctx 取消。
// n 大于窗口时按窗口容量分次占用(调用方无需拆分)。
func (w *Window) Acquire(ctx context.Context, n int64) error {
	for {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return ErrWindowClosed
		}
		if w.outstanding+min64(n, w.limit) <= w.limit {
			take := min64(n, w.limit)
			w.outstanding += take
			w.mu.Unlock()
			n -= take
			if n <= 0 {
				return nil
			}
			continue
		}
		ch := make(chan struct{}, 1)
		w.notify = append(w.notify, ch)
		w.mu.Unlock()

		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release 归还 n 字节额度,唤醒等待方。
func (w *Window) Release(n int64) {
	w.mu.Lock()
	// 允许为负:接收方超前归还形成信用 surplus,发送方后续直接消费
	w.outstanding -= n
	waiters := w.notify
	w.notify = nil
	w.mu.Unlock()
	for _, ch := range waiters {
		ch <- struct{}{}
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
