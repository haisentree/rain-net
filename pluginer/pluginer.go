package pluginer

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

var (
	// 服务器的类型:dns
	serverTypes = make(map[string]ServerType)
	// 插件
	Plugins = make(map[string]map[string]Plugin)

	// 进程退出时执行的函数列表
	OnProcessExit []func()

	instances []*Instance

	instancesMu sync.Mutex
)

func Start(yamlfile Input) (*Instance, error) {
	inst := &Instance{serverType: yamlfile.ServerType(), wg: new(sync.WaitGroup)}
	if err := ValidateAndExecuteDirectives(yamlfile, inst); err != nil {
		return nil, err
	}

	err := startWithListenerFds(inst)
	if err != nil {
		return inst, err
	}

	return inst, nil
}

func ValidateAndExecuteDirectives(yamlfile Input, inst *Instance) error {
	stype, ok := serverTypes[yamlfile.ServerType()]
	if !ok {
		return fmt.Errorf("no server types plugged in")
	}
	inst.context = stype.NewContext(inst)

	return executeDirectives(inst, inst.context)
}

// 加载配置信息
// func inspectServerBlocks() {}

// 把出现的插件按顺序都加载到instance.Context.Config中
func executeDirectives(inst *Instance, configer Configer) error {
	fmt.Println(configer.GetConfig())
	for _, srv := range configer.GetConfig().Service {
		if srv.Protocol != inst.serverType {
			continue
		}

		for _, host := range srv.Host {
			for _, plugin := range host.Plugin {
				controller := &Controller{
					instance:           inst,
					Key:                fmt.Sprintf("%s://%s", host.Network, host.Address),
					ServerBlockNetwork: host.Network,
					ServerBlockAddress: host.Address,
				}
				setup, err := DirectiveAction(inst.serverType, plugin)
				if err != nil {
					return err
				}

				err = setup(controller)
				if err != nil {
					return err
				}
			}
		}

	}
	return nil
}

func startWithListenerFds(inst *Instance) error {
	instancesMu.Lock()
	instances = append(instances, inst)
	instancesMu.Unlock()

	serverList, err := inst.context.MakeServers()
	if err != nil {
		return err
	}

	err = startServers(serverList, inst)
	if err != nil {
		return err
	}

	return nil
}

// 启动TCP和UDP服务器
func startServers(serverList []Server, inst *Instance) error {
	errChan := make(chan error, len(serverList))
	stopChan := make(chan struct{})
	stopWg := &sync.WaitGroup{}

	var (
		ln  net.Listener
		pc  net.PacketConn
		err error
	)

	for _, s := range serverList {
		if ln == nil {
			ln, err = s.Listen()
			if err != nil {
				return fmt.Errorf("Listen: %v", err)
			}
		}
		if pc == nil {
			pc, err = s.ListenPacket()
			if err != nil {
				return fmt.Errorf("ListenPacket: %v", err)
			}
		}
		inst.servers = append(inst.servers, ServerListener{server: s, listener: ln, packet: pc})
	}

	for _, s := range inst.servers {
		inst.wg.Add(2)
		stopWg.Add(2)
		func(s Server, ln net.Listener, pc net.PacketConn, inst *Instance) {
			go func() {
				defer func() {
					inst.wg.Done()
					stopWg.Done()
				}()
				errChan <- s.Serve(ln)
			}()

			go func() {
				defer func() {
					inst.wg.Done()
					stopWg.Done()
				}()
				errChan <- s.ServePacket(pc)
			}()
		}(s.server, s.listener, s.packet, inst)
	}

	go func() {
		for {
			select {
			case err := <-errChan:
				if err != nil {
					if !strings.Contains(err.Error(), "use of closed network connection") {
						// this error is normal when closing the listener; see https://github.com/golang/go/issues/4373
						log.Println(err)
					}
				}
			case <-stopChan:
				return
			}
		}
	}()

	go func() {
		stopWg.Wait()
		stopChan <- struct{}{}
	}()

	return nil
}
