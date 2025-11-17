package pluginer

import "sync"

// 服务管理的实例
type Instance struct {
	serverType string

	wg *sync.WaitGroup

	servers []ServerListener

	OnFirstStartup  []func() error // starting, not as part of a restart
	OnFinalShutdown []func() error // stopping, not as part of a restart

	Storage   map[interface{}]interface{}
	StorageMu sync.RWMutex
}

func (i *Instance) Wait() {
	i.wg.Wait()
}
