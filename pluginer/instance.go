package pluginer

import "sync"

// 服务管理的实例
type Instance struct {
	serverType string

	wg *sync.WaitGroup

	servers []ServerListener

	// these callbacks execute when certain events occur
	OnFirstStartup  []func() error // starting, not as part of a restart
	OnStartup       []func() error // starting, even as part of a restart
	OnRestart       []func() error // before restart commences
	OnRestartFailed []func() error // if restart failed
	OnShutdown      []func() error // stopping, even as part of a restart
	OnFinalShutdown []func() error // stopping, not as part of a restart

	Storage   map[interface{}]interface{}
	StorageMu sync.RWMutex
}

func (i *Instance) Wait() {
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
