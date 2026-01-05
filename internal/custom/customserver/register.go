package customserver

import (
	"errors"
	"fmt"
	"os"
	"rain-net/pluginer"

	"gopkg.in/yaml.v3"
)

const serverType = "custom"

func init() {
	pluginer.RegisterServerType(serverType, pluginer.ServerType{
		Directives:   newDirectives,
		DefaultInput: newDefaultInput,
		NewContext:   newContext,
	})
}

func newDirectives() []string {
	return []string{"test1"}
}

func newDefaultInput() pluginer.Input {
	return pluginer.YAMLFileInput{
		Filepath:       "/root/Project/DnsGit/rain-net/etc/custom.yaml",
		Contents:       []byte("default"),
		ServerTypeName: serverType,
	}
}

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

	return &customContext{
		Configs:       &config,
		ZoneToConfigs: make(map[string]*Config),
	}
}

var _ pluginer.Context = &customContext{}

type customContext struct {
	Configs       *Config
	ZoneToConfigs map[string]*Config
}

func (h *customContext) MakeServers() ([]pluginer.Server, error) {
	if len(h.Configs.Service) == 0 {
		return nil, errors.New("service is empty")
	}

	// h.propagateConfigParams(h.Configs)
	for zone, cfg := range h.ZoneToConfigs {
		fmt.Printf("zone: %s, config: %+v\n", zone, cfg)
	}

	servers, err := makeServersForGroup(h.Configs.Service)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

func (h *customContext) GetConfig() pluginer.Config {
	targetConfig := pluginer.Config{
		Service: make([]pluginer.Service, 0, len(h.Configs.Service)),
	}

	for _, srcSrv := range h.Configs.Service {

		targetSrv := pluginer.Service{
			Name:     srcSrv.Name,
			Protocol: srcSrv.Protocol,
			Host:     make([]pluginer.Host, 0, len(srcSrv.Host)),
		}
		for _, srcHost := range srcSrv.Host {
			targetHost := pluginer.Host{
				Network: srcHost.Network,
				Address: srcHost.Address,
				Plugin:  make([]string, len(srcHost.Plugin)),
			}

			copy(targetHost.Plugin, srcHost.Plugin)

			targetSrv.Host = append(targetSrv.Host, targetHost)
		}

		targetConfig.Service = append(targetConfig.Service, targetSrv)
	}

	return targetConfig
}

// 键值对传递[protocol://host]Config参数
// func (h *customContext) propagateConfigParams(configs *Config) {

// }

func makeServersForGroup(srvList []Service) ([]pluginer.Server, error) {
	var servers []pluginer.Server

	for _, srv := range srvList {
		if srv.Protocol != serverType {
			continue
		}

		for _, host := range srv.Host {
			switch host.Network {
			case "tcp":
				s, err := NewServer(srv.Name, host)
				if err != nil {
					fmt.Println("tcp NewServer err:", err.Error())
				}
				servers = append(servers, s)
			case "udp":
				s, err := NewServer(srv.Name, host)
				if err != nil {
					fmt.Println("udp NewServer err:", err.Error())
				}
				servers = append(servers, s)
			default:
				fmt.Println("error")
			}
		}
	}
	return servers, nil
}
