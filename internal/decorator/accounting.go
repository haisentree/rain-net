// Package decorator 提供基础 Conn 装饰器(design.md §九.6):
// 记账、限速、admission。横切面以 rainnet.Conn 包装器实现,
// Bridge 与协议层对装饰零感知(nps 参数膨胀教训的反面)。
package decorator

import (
	"sync/atomic"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

// Counters 是记账装饰器暴露的流量计数(原子,可直接被 admin/metrics 读取)。
type Counters struct {
	In  atomic.Uint64 // 从该连接读到的字节数(对端 → 本机)
	Out atomic.Uint64 // 写入该连接的字节数(本机 → 对端)
}

type accountingConn struct {
	rainnet.Conn
	in  *atomic.Uint64
	out *atomic.Uint64
}

func (a *accountingConn) Read(p []byte) (int, error) {
	n, err := a.Conn.Read(p)
	a.in.Add(uint64(n))
	return n, err
}

func (a *accountingConn) Write(p []byte) (int, error) {
	n, err := a.Conn.Write(p)
	a.out.Add(uint64(n))
	return n, err
}

// NewAccounting 包装连接并返回其流量计数器。
func NewAccounting(c rainnet.Conn) (rainnet.Conn, *Counters) {
	ct := &Counters{}
	return &accountingConn{Conn: c, in: &ct.In, out: &ct.Out}, ct
}
