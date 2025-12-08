package pluginer

import (
	"fmt"
	"log"
	"net"
	customserver "rain-net/internal/custom/server"
	"strings"
	"sync"
)

var (
	// 服务器的类型:dns
	serverTypes = make(map[string]ServerType)
	// 插件
	plugins = make(map[string]map[string]Plugin)

	// 进程退出时执行的函数列表
	OnProcessExit []func()

	// instances is the list of running Instances.
	instances []*Instance

	// instancesMu protects instances.
	instancesMu sync.Mutex
)

func Start() (*Instance, error) {
	inst := &Instance{serverType: "custom", wg: new(sync.WaitGroup), Storage: make(map[interface{}]interface{})}

	// startWithListenerFds
	tcpServerList, udpServerList, err := makeServers()
	if err != nil {
		return nil, err
	}

	// OnFirstStartup
	err = startServers(tcpServerList, udpServerList, inst)
	if err != nil {
		return nil, err
	}

	inst.Wait()

	return inst, nil
}

func makeServers() ([]TCPServer, []UDPServer, error) {
	var tcpServerList []TCPServer
	var udpServerList []UDPServer

	server, err := customserver.NewServer("")
	if err != nil {
		panic("make Server err")
	}
	tcpServerList = append(tcpServerList, server)
	udpServerList = append(udpServerList, server)

	return tcpServerList, udpServerList, nil
}

// 启动TCP和UDP服务器
func startServers(tcpServerList []TCPServer, udpServerList []UDPServer, inst *Instance) error {
	errChan := make(chan error, len(tcpServerList)+len(udpServerList))
	stopChan := make(chan struct{})
	stopWg := &sync.WaitGroup{}

	var (
		ln  net.Listener
		pc  net.PacketConn
		err error
	)

	for _, s := range tcpServerList {
		if ln == nil {
			ln, err = s.Listen()
			if err != nil {
				return fmt.Errorf("Listen: %v", err)
			}
		}
		inst.tcpServers = append(inst.tcpServers, TCPServerListener{server: s, listener: ln})
	}

	for _, s := range udpServerList {
		if pc == nil {
			pc, err = s.ListenPacket()
			if err != nil {
				return fmt.Errorf("ListenPacket: %v", err)
			}
		}
		inst.udpServers = append(inst.udpServers, UDPServerListener{server: s, packet: pc})
	}

	for _, s := range inst.tcpServers {
		inst.wg.Add(1)
		stopWg.Add(1)
		func(s TCPServer, ln net.Listener, inst *Instance) {
			go func() {
				defer func() {
					inst.wg.Done()
					stopWg.Done()
				}()
				errChan <- s.Serve(ln)
			}()
		}(s.server, s.listener, inst)
	}

	for _, s := range inst.udpServers {
		inst.wg.Add(1)
		stopWg.Add(1)
		func(s UDPServer, pc net.PacketConn, inst *Instance) {
			go func() {
				defer func() {
					inst.wg.Done()
					stopWg.Done()
				}()
				errChan <- s.ServePacket(pc)
			}()
		}(s.server, s.packet, inst)
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
