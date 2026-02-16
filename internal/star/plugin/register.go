package plugin

import "rain-net/pluginer"

func Register(name string, action pluginer.SetupFunc) {
	pluginer.RegisterPlugin(name, pluginer.Plugin{
		ServerType: "star",
		Action:     action,
	})
}
