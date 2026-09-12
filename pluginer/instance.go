package pluginer

import (
	"sync"
)

// 服务管理的实例
type Instance struct {
	serverType string
	input      Input

	wg *sync.WaitGroup

	servers []ServerListener

	// these callbacks execute when certain events occur
	OnFirstStartup  []func() error // starting, not as part of a restart
	OnStartup       []func() error // starting, even as part of a restart
	OnRestart       []func() error // before restart commences
	OnRestartFailed []func() error // if restart failed
	OnShutdown      []func() error // stopping, even as part of a restart
	OnFinalShutdown []func() error // stopping, not as part of a restart

	context   Context // 协议特定的配置项等
	StorageMu sync.RWMutex
}

func (i *Instance) Wait() {
	i.wg.Wait()
}

// Input 返回实例的配置输入(路径等)。
func (i *Instance) Input() Input { return i.input }

// Servers 返回实例承载的服务组件快照(测试与诊断用)。
func (i *Instance) Servers() []ServerListener {
	out := make([]ServerListener, len(i.servers))
	copy(out, i.servers)
	return out
}

// Server 返回服务组件本体。
func (sl ServerListener) Server() Server {
	return sl.server
}

// Stop 关闭全部监听/连接、执行 OnShutdown 回调,并等待服务退出。
// 热更新序列:Stop 旧实例 → 校验并 Start 新实例(设计 docs/design.md §八 热更新)。
func (i *Instance) Stop() {
	for _, sl := range i.servers {
		if sl.listener != nil {
			sl.listener.Close()
		}
		if sl.packet != nil {
			sl.packet.Close()
		}
	}
	_ = i.ShutdownCallbacks()
	i.wg.Wait()
}

func (i *Instance) ShutdownCallbacks() []error {
	var errs []error
	for _, shutdownFunc := range i.OnShutdown {
		err := shutdownFunc()
		if err != nil {
			errs = append(errs, err)
		}
	}
	for _, finalShutdownFunc := range i.OnFinalShutdown {
		err := finalShutdownFunc()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

type Context interface {
	MakeServers() ([]Server, error)
	GetConfig() Config
}
