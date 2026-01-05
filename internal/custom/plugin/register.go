package plugin

import "rain-net/pluginer"

// Register registers your plugin with CoreDNS and allows it to be called when the server is running.
func Register(name string, action pluginer.SetupFunc) {
	pluginer.RegisterPlugin(name, pluginer.Plugin{
		ServerType: "custom",
		Action:     action,
	})
}
