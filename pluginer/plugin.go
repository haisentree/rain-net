package pluginer

import "fmt"

type Plugin struct {
	ServerType string
	Action     SetupFunc
}

type SetupFunc func(c *Controller) error

func RegisterPlugin(name string, plugin Plugin) {
	if name == "" {
		panic("plugin must have a name")
	}
	if _, ok := Plugins[plugin.ServerType]; !ok {
		Plugins[plugin.ServerType] = make(map[string]Plugin)
	}
	if _, dup := Plugins[plugin.ServerType][name]; dup {
		panic("plugin named " + name + " already registered for server type " + plugin.ServerType)
	}
	Plugins[plugin.ServerType][name] = plugin
}

func DirectiveAction(serverType, dir string) (SetupFunc, error) {
	if stypePlugins, ok := Plugins[serverType]; ok {
		if plugin, ok := stypePlugins[dir]; ok {
			return plugin.Action, nil
		}
	}
	if genericPlugins, ok := Plugins[""]; ok {
		if plugin, ok := genericPlugins[dir]; ok {
			return plugin.Action, nil
		}
	}
	return nil, fmt.Errorf("no action found for directive '%s' with server type '%s' (missing a plugin?)",
		dir, serverType)
}
