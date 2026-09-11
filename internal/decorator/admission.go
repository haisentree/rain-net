package decorator

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

var (
	errAdmissionFull = errors.New("admission: connection limit reached")
	errAdmissionRate = errors.New("admission: identity rate exceeded")
)

// Gate 是 admission 装饰器:接入准入门(design.md §九.6)。
//   - maxConns:并发连接数上限(0 = 不限);
//   - identityQPS:单身份每秒新建连接数上限(0 = 不限),超限为策略拒绝。
type Gate struct {
	maxConns int64
	identity float64
	cur      atomic.Int64

	mu      sync.Mutex
	buckets map[string]*bucket
}

func NewGate(maxConns int64, identityQPS float64) *Gate {
	return &Gate{maxConns: maxConns, identity: identityQPS, buckets: map[string]*bucket{}}
}

// Acquire 申请接入一条连接:并发满或身份超速 → 策略拒绝(rainnet.KindPolicy)。
// 成功后必须配对调用 Release。
func (g *Gate) Acquire(identity string) error {
	for {
		cur := g.cur.Load()
		if g.maxConns > 0 && cur >= g.maxConns {
			return rainnet.PolicyError(errAdmissionFull)
		}
		if g.cur.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	if g.identity > 0 && identity != "" {
		g.mu.Lock()
		b := g.buckets[identity]
		if b == nil {
			b = newBucket(g.identity)
			g.buckets[identity] = b
		}
		g.mu.Unlock()
		if !b.tryTake(1) {
			g.Release()
			return rainnet.PolicyError(errAdmissionRate)
		}
	}
	return nil
}

// Release 释放一个并发配额。
func (g *Gate) Release() { g.cur.Add(-1) }

// InUse 当前并发连接数(观测用)。
func (g *Gate) InUse() int64 { return g.cur.Load() }
