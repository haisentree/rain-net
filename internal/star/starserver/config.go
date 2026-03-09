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
	DailerList   []DailerList   `yaml:"dailerList"`

	ListenerMap map[string]ListenerList
	HandlerMap  map[string]HandlerList
	DailerMap   map[string]DailerList

	Plugin      []plugin.Plugin
	PluginChain plugin.Handler
	Registry    map[string]plugin.Handler
}

type Service struct {
	Name      string      `yaml:"name"`
	Type      string      `yaml:"type"`
	Listeners []Listeners `yaml:"listeners"`
	Dailers   []string    `yaml:"dailers"`
}

type ListenerList struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	Transport string `yaml:"transport,omitempty"`
	Addr      string `yaml:"addr"`

	Settings Settings `yaml:"settings"`
}
type DailerList struct {
	Name            string `yaml:"name"`
	Type            string `yaml:"type"`
	Transport       string `yaml:"transport,omitempty"`
	Addr            string `yaml:"addr"`
	ClientProxyName string `yaml:"clientProxyName"`
	KeyPassword     string `yaml:"keyPassword"`
}

type Settings struct {
	CtrlProxyName string        `yaml:"ctrlProxyName,omitempty"`
	Connect       []ConnectItem `yaml:"connect,omitempty"`
	ClientProxy   []ClientProxy `yaml:"clientProxy,omitempty"`
}

type ConnectItem struct {
	ProxyName       string `yaml:"proxyName"`
	BridgeName      string `yaml:"bridgeName"`
	ClientProxyName string `yaml:"clientProxyName"`
	StreamId        string `yaml:"streamId"`
}

type ClientProxy struct {
	ClientProxyName string `yaml:"clientProxyName"`
	StreamId        string `yaml:"streamId"`
	Addr            string `yaml:"addr"`
	Transport       string `yaml:"transport"`
	KeyPassword     string `yaml:"keyPassword,omitempty"`
}

type HandlerList struct {
	Name    string   `yaml:"name"`
	Plugins []string `yaml:"plugins"`
}

type Listeners struct {
	ListenerName string `yaml:"listenerName"`
	HandlerName  string `yaml:"handlerName"`
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
