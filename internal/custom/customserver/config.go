package customserver

import (
	"rain-net/internal/custom/plugin"
	"rain-net/pluginer"
)

// 协议特有的配置
type Config struct {
	Service []Service `yaml:"service"` // 服务列表

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

func (c *Config) AddPlugin(m plugin.Plugin) {
	c.Plugin = append(c.Plugin, m)
}

func GetConfig(c *pluginer.Controller) *Config {
	ctx := c.Context().(*customContext)
	// key := keyForConfig(c.ServerBlockIndex, c.ServerBlockKeyIndex)
	// if cfg, ok := ctx.keysToConfigs[key]; ok {
	// 	return cfg
	// }
	// // we should only get here during tests because directive
	// // actions typically skip the server blocks where we make
	// // the configs.
	// ctx.saveConfig(key, &Config{ListenHosts: []string{""}})
	// return GetConfig(c)
	if ctx.Configs == nil {
		ctx.Configs = &Config{}
	}
	return GetConfig(c)
}
