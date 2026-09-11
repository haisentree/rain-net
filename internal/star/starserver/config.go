package starserver

import (
	"fmt"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/star/plugin"
	"github.com/haisentree/rain-net/pluginer"
)

// 配置模型类型定义在 config 包(纯 YAML schema),此处以别名引用,
// 保持包内既有代码与测试的引用方式稳定;字段级重命名随新 schema 切换一并处理。
type (
	Service      = config.Service
	Listeners    = config.Listeners
	ListenerList = config.ListenerList
	HandlerList  = config.HandlerList
	DailerList   = config.DailerList
	Settings     = config.Settings
	ConnectItem  = config.ConnectItem
	ClientProxy  = config.ClientProxy
)

type Config struct {
	Service      []Service      `yaml:"service"` // 服务列表
	ListenerList []ListenerList `yaml:"listenerList"`
	HandlerList  []HandlerList  `yaml:"handlerList"`
	DailerList   []DailerList   `yaml:"dailerList"`

	ListenerMap map[string]*ListenerList
	HandlerMap  map[string]HandlerList
	DailerMap   map[string]DailerList

	// Routes 由新 schema routes 按 dialer 展开而来的 connect 规则(非 YAML 字段)
	Routes []ConnectItem

	// ChainRoutes 出站链路路由(role: outbound 的 star dialer,非 YAML 字段)
	ChainRoutes []ChainRoute
	DialerSpecs map[string]config.DialerSpec

	Plugin      []plugin.Plugin
	PluginChain plugin.Handler
	Registry    map[string]plugin.Handler
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

// ChainRoute 出站链路路由:proxy 名 → 链路拨号器(内嵌客户端拨向远端节点)
type ChainRoute struct {
	ProxyName string // 匹配的 ingress 名
	Dialer    string // dialers[] 名称
	Identity  string // 远端凭据身份
	StreamId  string // 远端流
}

func keyForConfig(blocIndex string, blocKeyIndex string) string {
	return fmt.Sprintf("%s://%s", blocIndex, blocKeyIndex)
}

// findListenerSettings 按 网络+地址 查找监听器设置
func findListenerSettings(h *starContext, network, addr string) Settings {
	for i := range h.Configs.ListenerList {
		l := &h.Configs.ListenerList[i]
		if l.Addr == addr && (l.Transport == network || l.Type == network) {
			return l.Settings
		}
	}
	return Settings{}
}

// GetListenerSettings 供插件在 setup 阶段读取所在监听器的 settings
// (如 forward 插件读取 target)
func GetListenerSettings(c *pluginer.Controller) Settings {
	ctx, ok := c.Context().(*starContext)
	if !ok {
		return Settings{}
	}
	return findListenerSettings(ctx, c.ServerBlockNetwork, c.ServerBlockAddress)
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
