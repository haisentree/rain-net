package custom

import (
	"rain-net/pluginer"

	"github.com/coredns/caddy"
)

const serverType = "custom"

func init() {
	pluginer.RegisterServerType(serverType, pluginer.ServerType{
		Directives: newDirectives,
		DefaultInput: func() caddy.Input {
			return caddy.CaddyfileInput{
				Filepath:       "Corefile",
				Contents:       []byte(".:" + Port + " {\nwhoami\nlog\n}\n"),
				ServerTypeName: serverType,
			}
		},
		NewContext: newContext,
	})
}

func newDirectives() []string {
	return []string{"test1"}
}

func newContext() {

}

// func newContext(i *caddy.Instance) caddy.Context {
// 	return &dnsContext{keysToConfigs: make(map[string]*Config)}
// }

type customContext struct {
	configs []*Config
}

func (h *customContext) MakeServers() ([]pluginer.Server, error) {
	return nil, nil
}
