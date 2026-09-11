package decorator

import (
	"context"
	"math"
	"sync"
	"time"
)

// bucket 是最小令牌桶:固定速率补充,桶容量 burst = 一秒配额。
// 同时服务字节限速(Read/Write 前扣令牌)与 admission 的按身份 QPS(每次请求扣 1)。
type bucket struct {
	mu     sync.Mutex
	rate   float64 // 每秒补充令牌数,也是桶容量
	tokens float64 // 当前令牌
	last   time.Time
}

func newBucket(rate float64) *bucket {
	return &bucket{rate: rate, tokens: rate}
}

// take 取 n 个令牌;不足时等待补充后继续取,直到取满(受 ctx 保护)。
// n 可大于桶容量:分多次取,每次至多取空桶。
func (b *bucket) take(ctx context.Context, n float64) error {
	remaining := n
	for {
		b.mu.Lock()
		now := time.Now()
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.rate {
			b.tokens = b.rate
		}
		b.last = now
		took := math.Min(b.tokens, remaining)
		if took > 0 {
			b.tokens -= took
			remaining -= took
		}
		b.mu.Unlock()

		if remaining <= 1e-9 {
			return nil
		}
		// 等待补充,分片睡眠保证 ctx 可中断
		wait := time.Duration(1 / b.rate * float64(time.Second))
		if wait > 100*time.Millisecond {
			wait = 100 * time.Millisecond
		}
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// tryTake 非阻塞取令牌,不足返回 false(admission 用)。
func (b *bucket) tryTake(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.rate {
		b.tokens = b.rate
	}
	b.last = now
	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}
