package starserver

import (
	"fmt"
	"rain-net/internal/star/plugin"
	"rain-net/pluginer"
)

type Config struct {
	Service      []Service      `yaml:"service"` // 服务列表
	ListenerList []ListenerList `yaml:"listenerList"`
	HandlerList  []HandlerList  `yaml:"handlerList"`

	Plugin      []plugin.Plugin
	PluginChain plugin.Handler
	Registry    map[string]plugin.Handler
}

type Service struct {
	Name      string      `yaml:"name"`
	Type      string      `yaml:"type"`
	Listeners []Listeners `yaml:"listeners"`
}

type ListenerList struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Addr string `yaml:"addr"`
}

type HandlerList struct {
	Name     string   `yaml:"name"`
	Protocol []string `yaml:"protocol"`
}

type Listeners struct {
	ListenerName string `yaml:"listenerName"`
	HandlerName  string `yaml:"handlerName"`
}

func (c *Config) GetConfig() pluginer.Config {
	targetConfig := pluginer.Config{
		Service: make([]pluginer.Service, 0, len(c.Service)),
	}

	ListenerMap := make(map[string]ListenerList, len(c.ListenerList))
	HandlerMap := make(map[string]HandlerList, len(c.HandlerList))

	for _, val := range c.ListenerList {
		ListenerMap[val.Name] = val
	}
	for _, val := range c.HandlerList {
		HandlerMap[val.Name] = val
	}

	for _, srcSrv := range c.Service {

		targetSrv := pluginer.Service{
			Name:     srcSrv.Name,
			Protocol: srcSrv.Type,
			Host:     make([]pluginer.Host, 0, len(srcSrv.Listeners)),
		}
		for _, val := range srcSrv.Listeners {
			targetHost := pluginer.Host{
				Network: ListenerMap[val.ListenerName].Type,
				Address: ListenerMap[val.ListenerName].Addr,
				Plugin:  make([]string, 1),
			}

			copy(targetHost.Plugin, HandlerMap[val.ListenerName].Protocol)

			targetSrv.Host = append(targetSrv.Host, targetHost)
		}

		targetConfig.Service = append(targetConfig.Service, targetSrv)
	}
	return targetConfig
}

func (c *Config) AddPlugin(m plugin.Plugin) {
	c.Plugin = append(c.Plugin, m)
}

func (c *Config) RegisterHandler(h plugin.Handler) {
	if c.Registry == nil {
		c.Registry = make(map[string]plugin.Handler)
	}

	c.Registry[h.Name()] = h
}

func (c *Config) Handler(name string) plugin.Handler {
	if c.Registry == nil {
		return nil
	}
	if h, ok := c.Registry[name]; ok {
		return h
	}
	return nil
}

func (c *Config) Handlers() []plugin.Handler {
	if c.Registry == nil {
		return nil
	}
	hs := make([]plugin.Handler, 0, len(c.Registry))
	for _, k := range Directives {
		registry := c.Handler(k)
		if registry != nil {
			hs = append(hs, registry)
		}
	}
	return hs
}

func keyForConfig(blocIndex string, blocKeyIndex string) string {
	return fmt.Sprintf("%s://%s", blocIndex, blocKeyIndex)
}

func GetConfig(c *pluginer.Controller) *Config {
	ctx := c.Context().(*starContext)
	key := keyForConfig(c.ServerBlockNetwork, c.ServerBlockAddress)
	if cfg, ok := ctx.ZoneToConfigs[key]; ok {
		return cfg
	}
	ctx.ZoneToConfigs[key] = &Config{}

	return GetConfig(c)
}
