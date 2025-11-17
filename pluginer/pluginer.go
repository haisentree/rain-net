package pluginer

import (
	"fmt"
	"log"
	"net"
	customP "rain-net/internal/custom"
	"strings"
	"sync"
)

var (
	// 服务器的类型:dns
	serverTypes = make(map[string]ServerType)
	// 插件
	plugins = make(map[string]map[string]Plugin)
)

func Start() (*Instance, error) {
	inst := &Instance{serverType: "custom", wg: new(sync.WaitGroup), Storage: make(map[interface{}]interface{})}

	serverList, err := makeServers()
	if err != nil {
		return nil, err
	}

	err = startServers(serverList, inst)
	if err != nil {
		return nil, err
	}

	return inst, nil
}

func makeServers() ([]Server, error) {
	var serverList []Server
	server, err := customP.NewServer("")
	if err != nil {
		panic("make Server err")
	}
	serverList = append(serverList, server)
	return serverList, nil
}

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
