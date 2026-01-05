package customserver

import (
	"fmt"
	"rain-net/internal/custom/plugin"

	// "rain-net/internal/custom/zplugin"
	"rain-net/pluginer"
)

// 协议特有的配置
// TODO: 分离yaml配置和运行时配置
type Config struct {
	Service     []Service `yaml:"service"` // 服务列表
	Plugin      []plugin.Plugin
	PluginChain plugin.Handler
	Registry    map[string]plugin.Handler
}

type Service struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Host     []Host `yaml:"host"`
}

type Host struct {
	Key     string   `yaml:"key"`
	Network string   `yaml:"network"`
	Address string   `yaml:"address"`
	Plugin  []string `yaml:"plugin"`
}

func (c *Config) GetConfig() pluginer.Config {
	targetConfig := pluginer.Config{
		Service: make([]pluginer.Service, 0, len(c.Service)),
	}

	for _, srcSrv := range c.Service {

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

// Handlers returns a slice of plugins that have been registered. This can be used to
// inspect and interact with registered plugins but cannot be used to remove or add plugins.
// Note that this is order dependent and the order is defined in directives.go, i.e. if your plugin
// comes before the plugin you are checking; it will not be there (yet).
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
	ctx := c.Context().(*customContext)
	key := keyForConfig(c.ServerBlockNetwork, c.ServerBlockAddress)
	if cfg, ok := ctx.ZoneToConfigs[key]; ok {
		return cfg
	}
	ctx.ZoneToConfigs[key] = &Config{}

	return GetConfig(c)
}
