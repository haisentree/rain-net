package pluginer

type Plugin struct {
	ServerType string
	Action     SetupFunc
}

type SetupFunc func(c *Controller) error

func RegisterPlugin(name string, plugin Plugin) {
	if name == "" {
		panic("plugin must have a name")
	}
	if _, ok := plugins[plugin.ServerType]; !ok {
		plugins[plugin.ServerType] = make(map[string]Plugin)
	}
	if _, dup := plugins[plugin.ServerType][name]; dup {
		panic("plugin named " + name + " already registered for server type " + plugin.ServerType)
	}
	plugins[plugin.ServerType][name] = plugin
}
