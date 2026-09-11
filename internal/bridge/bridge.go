// Package bridge 实现连接桥接:把两条已建立的全双工连接接在一起双向搬运字节。
//
// 职责口诀(docs/design.md §二):Dialer 负责"怎么建立",Bridge 负责"建立之后"。
// Bridge 是哑字节泵——不拨号、不解析协议,只做三件事:
//  1. 双向 io.Copy(池化 buffer,io.Copy 在 Linux 上对 TCP 自动走 splice);
//  2. 半关闭传播:一方向读到 EOF,对侧 Conn.CloseWrite()(不支持则降级为整条关闭),
//     保证"客户端关写、等服务端完整应答"类协议的字节不被提前掐断;
//  3. 关闭治理:任一方向出错立即双关;首方向 EOF 后受 DrainTimeout 保护,
//     防止对端挂死导致排空无限等;ctx 取消立即双关并返回 ctx.Err()。
//
// 形状取 gost 的纯函数(见 docs/research/bridge.md),横切面(记账/限速)不进签名,
// 以 rainnet.Conn 装饰器方式包在传入连接外层。
package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

const (
	defaultDrainTimeout = 30 * time.Second
	defaultBufferSize   = 64 << 10
)

// Options 桥接参数。
type Options struct {
	// DrainTimeout 首方向 EOF(半关闭)后等待另一方向结束的上限;0 表示不限时。
	DrainTimeout time.Duration
	// BufferSize 单向拷贝缓冲大小,默认 64KB。
	BufferSize int
}

type Option func(*Options)

func WithDrainTimeout(d time.Duration) Option { return func(o *Options) { o.DrainTimeout = d } }
func WithBufferSize(n int) Option             { return func(o *Options) { o.BufferSize = n } }

var bufPool = sync.Pool{New: func() any { return make([]byte, defaultBufferSize) }}

func getBuf(size int) []byte {
	if size == defaultBufferSize {
		return bufPool.Get().([]byte)
	}
	return make([]byte, size)
}

func putBuf(size int, b []byte) {
	if size == defaultBufferSize && cap(b) == defaultBufferSize {
		bufPool.Put(b)
	}
}

// Bridge 双向桥接 src 与 dst,返回首个非 EOF 错误;返回时两条连接都已被关闭。
//
// 半关闭传播矩阵(docs/design.md §二):
//
//	源侧事件          | 目的侧支持 CloseWrite     | 目的侧不支持
//	------------------+--------------------------+---------------------
//	CloseWrite        | 转发 CloseWrite,继续读   | 记"已关写",继续读
//	读到 EOF          | CloseWrite 后排空再关     | 排空后整条关闭(降级)
//	读错误/取消       | 立即双关                  | 立即双关
func Bridge(ctx context.Context, src, dst rainnet.Conn, opts ...Option) error {
	o := Options{DrainTimeout: defaultDrainTimeout, BufferSize: defaultBufferSize}
	for _, f := range opts {
		f(&o)
	}
	if o.BufferSize <= 0 {
		o.BufferSize = defaultBufferSize
	}

	defer src.Close()
	defer dst.Close()

	// ctx 取消:立即双关,保证两个 pump 在有限时间内退出
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			src.Close()
			dst.Close()
		case <-stop:
		}
	}()

	type done struct{ err error }
	results := make(chan done, 2)

	// 排空保护:首方向完成后启动,DrainTimeout 内另一方向必须收尾
	var drainOnce sync.Once
	drainDone := make(chan struct{})
	var timedOut atomic.Bool
	armDrain := func() {
		drainOnce.Do(func() {
			if o.DrainTimeout <= 0 {
				return
			}
			go func() {
				t := time.NewTimer(o.DrainTimeout)
				defer t.Stop()
				select {
				case <-drainDone:
				case <-t.C:
					timedOut.Store(true)
					src.Close()
					dst.Close()
				}
			}()
		})
	}

	var pumpOnce sync.Once
	finishDrain := func() {
		pumpOnce.Do(func() { close(drainDone) })
	}

	pump := func(name string, r io.Reader, w rainnet.Conn) {
		buf := getBuf(o.BufferSize)
		_, err := io.CopyBuffer(w, r, buf)
		putBuf(o.BufferSize, buf)

		var resErr error
		if err != nil && !errors.Is(err, io.EOF) {
			resErr = rainnet.NetworkError(fmt.Errorf("bridge %s: %w", name, err))
		} else if cerr := w.CloseWrite(); cerr != nil && !errors.Is(cerr, rainnet.ErrNotSupported) {
			resErr = rainnet.NetworkError(fmt.Errorf("bridge %s CloseWrite: %w", name, cerr))
		}
		armDrain()
		results <- done{resErr}
	}

	go pump("src->dst", src, dst)
	go pump("dst->src", dst, src)

	first := <-results
	if ctx.Err() != nil {
		finishDrain()
		return ctx.Err()
	}
	if first.err != nil {
		// 出错即双关(defer 兜底),第二个 pump 随关闭而退出,缓冲通道不会泄漏
		finishDrain()
		return first.err
	}
	// 第一方向 EOF:半关闭已传播,等第二方向收尾(受 DrainTimeout 保护)
	second := <-results
	finishDrain()
	if timedOut.Load() {
		return rainnet.NetworkError(fmt.Errorf("bridge: drain timeout after %s", o.DrainTimeout))
	}
	return second.err
}
