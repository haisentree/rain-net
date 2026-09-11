// Package router 是转发决策组件(design.md §二):
// 按 (ingress, identity, target) 匹配规则选出出站 dialer。
//
// 匹配语义(§九.5,已定稿):按规则声明顺序匹配、首中即停,不做最长匹配——
// 可预测,且与 admin API 动态增删的顺序语义一致。空字段 = 通配。
package router

import (
	"sync"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

// Rule 一条路由规则。三个匹配字段为空都表示通配。
type Rule struct {
	Ingress  string   // 匹配 Meta.Listener
	Identity string   // 匹配 Meta.Identity
	Target   string   // 匹配 Meta.Target
	Dialer   string   // 命中后使用的出站 dialer 名
	Streams  []string // star 目标流集合;多条时 round-robin(故障转移 = 流下线后由调用方移除)
}

// Decision 一次匹配的决策结果。
type Decision struct {
	Dialer string
	Stream string // Streams 为空时为 ""
	Rule   Rule   // 命中的规则(含完整匹配字段,便于日志)
}

type Router struct {
	mu      sync.RWMutex
	rules   []Rule
	cursors map[int]int // 规则下标 -> round-robin 游标
}

func New() *Router {
	return &Router{cursors: map[int]int{}}
}

// Seed 批量写入规则(启动种子),重复规则跳过。
func (r *Router) Seed(rules []Rule) {
	for _, rule := range rules {
		_ = r.Add(rule)
	}
}

// sameRule 规则等价:匹配三元组 + 出站 + 流集合一致。
func sameRule(a, b Rule) bool {
	if a.Ingress != b.Ingress || a.Identity != b.Identity || a.Target != b.Target ||
		a.Dialer != b.Dialer || len(a.Streams) != len(b.Streams) {
		return false
	}
	for i := range a.Streams {
		if a.Streams[i] != b.Streams[i] {
			return false
		}
	}
	return true
}

// Add 动态添加规则,重复时返回 false。
func (r *Router) Add(rule Rule) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, exist := range r.rules {
		if sameRule(exist, rule) {
			return false
		}
	}
	r.rules = append(r.rules, rule)
	return true
}

// Remove 删除一条完全等价的规则,返回是否存在。
func (r *Router) Remove(rule Rule) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, exist := range r.rules {
		if sameRule(exist, rule) {
			r.rules = append(r.rules[:i], r.rules[i+1:]...)
			delete(r.cursors, i)
			return true
		}
	}
	return false
}

// Rules 返回规则快照(声明顺序)。
func (r *Router) Rules() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Rule, len(r.rules))
	copy(out, r.rules)
	return out
}

// Match 按声明顺序首中即停;命中多条流的规则时按 round-robin 选一条。
func (r *Router) Match(m rainnet.Meta) (Decision, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, rule := range r.rules {
		if !ruleMatches(rule, m) {
			continue
		}
		d := Decision{Dialer: rule.Dialer, Rule: rule}
		if n := len(rule.Streams); n > 0 {
			d.Stream = rule.Streams[r.cursors[i]%n]
			r.cursors[i] = (r.cursors[i] + 1) % n
		}
		return d, true
	}
	return Decision{}, false
}

func ruleMatches(rule Rule, m rainnet.Meta) bool {
	return (rule.Ingress == "" || rule.Ingress == m.Listener) &&
		(rule.Identity == "" || rule.Identity == m.Identity) &&
		(rule.Target == "" || rule.Target == m.Target)
}
