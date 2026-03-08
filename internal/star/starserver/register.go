package starserver

import (
	"errors"
	"fmt"
	"os"
	"rain-net/pluginer"

	"gopkg.in/yaml.v3"
)

const serverType = "star"

func init() {
	pluginer.RegisterServerType(serverType, pluginer.ServerType{
		Directives:   newDirectives,
		DefaultInput: newDefaultInput,
		NewContext:   newContext,
	})
}

func newDirectives() []string {
	return []string{"socks5"}
}

func newDefaultInput() pluginer.Input {
	return pluginer.YAMLFileInput{
		Filepath:       "/root/Project/rain-net/etc/star.example.yaml",
		Contents:       []byte("default"),
		ServerTypeName: serverType,
	}
}

// 初始化特定服务的Config
func newContext(i *pluginer.Instance) pluginer.Context {
	data, err := os.ReadFile(newDefaultInput().Path())
	if err != nil {
		panic(err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		panic(err)
	}

	config.ListenerMap = make(map[string]ListenerList, len(config.ListenerList))
	config.HandlerMap = make(map[string]HandlerList, len(config.HandlerList))

	for _, val := range config.ListenerList {
		config.ListenerMap[val.Name] = val
	}
	for _, val := range config.HandlerList {
		config.HandlerMap[val.Name] = val
	}

	fmt.Printf("config: %s\n", config.ListenerList[0].Name)

	return &starContext{
		Configs:       &config,
		ZoneToConfigs: make(map[string]*Config),
	}
}

var _ pluginer.Context = &starContext{}

type starContext struct {
	Configs       *Config
	ZoneToConfigs map[string]*Config
}

func (h *starContext) MakeServers() ([]pluginer.Server, error) {
	if len(h.Configs.Service) == 0 {
		return nil, errors.New("service is empty")
	}

	for zone, cfg := range h.ZoneToConfigs {
		fmt.Printf("zone: %s, config: %+v\n", zone, cfg)
	}

	servers, err := h.makeServersForGroup(h.Configs.Service)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

// func (h *starContext) GetConfig() pluginer.Config {
// 	return h.Configs.GetConfig()
// }

func (h *starContext) GetConfig() pluginer.Config {
	targetConfig := pluginer.Config{
		Service: make([]pluginer.Service, 0, len(h.Configs.Service)),
	}

	ListenerMap := make(map[string]ListenerList, len(h.Configs.ListenerList))
	HandlerMap := make(map[string]HandlerList, len(h.Configs.HandlerList))

	for _, val := range h.Configs.ListenerList {
		ListenerMap[val.Name] = val
	}
	for _, val := range h.Configs.HandlerList {
		HandlerMap[val.Name] = val
	}

	for _, srcSrv := range h.Configs.Service {

		targetSrv := pluginer.Service{
			Name:        srcSrv.Name,
			ServiceType: srcSrv.Type,
			Host:        make([]pluginer.Host, 0, len(srcSrv.Listeners)),
		}
		for _, val := range srcSrv.Listeners {
			targetHost := pluginer.Host{
				// Network: ListenerMap[val.ListenerName].Type,
				Network: ListenerMap[val.ListenerName].Transport,
				Address: ListenerMap[val.ListenerName].Addr,
				Plugin:  HandlerMap[val.HandlerName].Plugins,
			}

			copy(targetHost.Plugin, HandlerMap[val.ListenerName].Plugins)

			targetSrv.Host = append(targetSrv.Host, targetHost)
		}

		targetConfig.Service = append(targetConfig.Service, targetSrv)
	}
	return targetConfig
}

// func (h *starContext) makeServersForGroup(srvList []Service) ([]pluginer.Server, error) {
// 	var servers []pluginer.Server

// 	for _, srv := range h.GetConfig().Service {
// 		if srv.ServiceType != serverType {
// 			continue
// 		}

// 		for _, host := range srv.Host {
// 			switch host.Network {
// 			case "tcp":
// 				key := fmt.Sprintf("%s://%s", host.Network, host.Address)
// 				s, err := NewServer(srv.Name, host, h.ZoneToConfigs[key])
// 				if err != nil {
// 					fmt.Println("tcp NewServer err:", err.Error())
// 				}
// 				servers = append(servers, s)
// 			case "udp":
// 				key := fmt.Sprintf("%s://%s", host.Network, host.Address)
// 				s, err := NewServer(srv.Name, host, h.ZoneToConfigs[key])
// 				if err != nil {
// 					fmt.Println("udp NewServer err:", err.Error())
// 				}
// 				servers = append(servers, s)
// 			case "socks5":
// 				key := fmt.Sprintf("%s://%s", host.Network, host.Address)
// 				s, err := NewServer(srv.Name, host, h.ZoneToConfigs[key])
// 				if err != nil {
// 					fmt.Println("socks5 NewServer err:", err.Error())
// 				}
// 				servers = append(servers, s)
// 			default:
// 				fmt.Println("error")
// 			}
// 		}
// 	}
// 	return servers, nil
// }

// 根据特定服务的配置makeserver
func (h *starContext) makeServersForGroup(srvList []Service) ([]pluginer.Server, error) {
	var servers []pluginer.Server

	for _, srv := range srvList {
		if srv.Type != serverType {
			continue
		}

		for _, ls := range srv.Listeners {
			listener := h.Configs.ListenerMap[ls.ListenerName]
			key := fmt.Sprintf("%s://%s", listener.Transport, listener.Addr)
			switch listener.Transport {
			case "tcp":
				s, err := NewServer(srv.Name, listener.Transport, listener.Addr, h.ZoneToConfigs[key])
				if err != nil {
					fmt.Println("tcp NewServer err:", err.Error())
				}
				servers = append(servers, s)
			case "udp":
				s, err := NewServer(srv.Name, listener.Transport, listener.Addr, h.ZoneToConfigs[key])
				if err != nil {
					fmt.Println("udp NewServer err:", err.Error())
				}
				servers = append(servers, s)
			default:
				panic(fmt.Sprintf("unsupported transport: %s", listener.Transport))
			}
		}
	}
	return servers, nil
}

func (h *starContext) makeListeners() ([]pluginer.Server, error) {
	return nil, nil
}

func (s *starContext) makeBridgers() (err error) {
	return nil
}

func (s *starContext) makeDailers() (err error) {
	return nil
}
