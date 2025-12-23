package customserver

import "rain-net/internal/custom/plugin"

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
	Key     string `yaml:"key"`
	Network string `yaml:"network"`
	Address string `yaml:"address"`
}
