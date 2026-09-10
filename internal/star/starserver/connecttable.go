package starserver

import (
	"sync"
)

// ConnectTable connect 路由规则的运行时注册表
//
// 启动时用配置文件里的静态规则做种子,之后 admin API 可以动态增删,
// ProxyServer 每次选路时实时读取——规则变更无需重启
type ConnectTable struct {
	mu    sync.RWMutex
	rules map[string][]ConnectItem // proxyName -> 规则列表
}

func NewConnectTable() *ConnectTable {
	return &ConnectTable{rules: make(map[string][]ConnectItem)}
}

// sameConnectRule 判断两条规则是否等价(三元组唯一)
func sameConnectRule(a, b ConnectItem) bool {
	return a.ProxyName == b.ProxyName &&
		a.ClientProxyName == b.ClientProxyName &&
		a.StreamId == b.StreamId
}

// Seed 批量写入规则(启动种子)
func (t *ConnectTable) Seed(rules []ConnectItem) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range rules {
		list := t.rules[r.ProxyName]
		dup := false
		for _, exist := range list {
			if sameConnectRule(exist, r) {
				dup = true
				break
			}
		}
		if !dup {
			t.rules[r.ProxyName] = append(list, r)
		}
	}
}

// Add 动态添加一条规则,重复时返回 false
func (t *ConnectTable) Add(rule ConnectItem) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, exist := range t.rules[rule.ProxyName] {
		if sameConnectRule(exist, rule) {
			return false
		}
	}
	t.rules[rule.ProxyName] = append(t.rules[rule.ProxyName], rule)
	return true
}

// Remove 删除一条规则,返回是否存在
func (t *ConnectTable) Remove(proxyName, clientProxyName, streamId string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	list := t.rules[proxyName]
	for i, exist := range list {
		if exist.ClientProxyName == clientProxyName && exist.StreamId == streamId {
			t.rules[proxyName] = append(list[:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// For 返回某 proxy 监听器的规则副本
func (t *ConnectTable) For(proxyName string) []ConnectItem {
	t.mu.RLock()
	defer t.mu.RUnlock()

	list := t.rules[proxyName]
	out := make([]ConnectItem, len(list))
	copy(out, list)
	return out
}

// All 返回全部规则的快照,按 proxyName 分组
func (t *ConnectTable) All() map[string][]ConnectItem {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[string][]ConnectItem, len(t.rules))
	for name, list := range t.rules {
		cp := make([]ConnectItem, len(list))
		copy(cp, list)
		out[name] = cp
	}
	return out
}
