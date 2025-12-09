package customserver

import (
	"errors"
	"fmt"
	"rain-net/pluginer"
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
		Contents:       []byte("content"),
		ServerTypeName: serverType,
	}
}

func newContext(i *pluginer.Instance) pluginer.Context {
	return &customContext{}
}

type customContext struct {
	Configs *Config
}

func (h *customContext) MakeServers() ([]pluginer.Server, error) {
	if len(h.Configs.Service) == 0 {
		return nil, errors.New("service is empty")
	}

	servers, err := makeServersForGroup(h.Configs.Service)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

func makeServersForGroup(srvList []Service) ([]pluginer.Server, error) {
	for _, srv := range srvList {
		for _, host := range srv.Host {
			switch host.Network {
			case "tcp":
			case "udp":
			default:
				fmt.Println("error")
			}
		}
	}
	return nil, nil
}
