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

	ListenerMap map[string]*ListenerList
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
	TLS             bool   `yaml:"tls,omitempty"`           // 与 ctrl 通道建立 TLS 连接
	TLSSkipVerify   bool   `yaml:"tlsSkipVerify,omitempty"` // 跳过服务端证书校验(自签名证书场景)
}

type Settings struct {
	CtrlProxyName string `yaml:"ctrlProxyName,omitempty"`
	ProxyName     string `yaml:"proxyName,omitempty"`     // proxy 监听器的逻辑代理名,用于匹配 connect 规则
	KeyPassword   string `yaml:"keyPassword,omitempty"`   // ctrlproxy 管理员密码,注册不限 clientProxyName
	AdminPassword string `yaml:"adminPassword,omitempty"` // admin 监听器的接口鉴权 token(Bearer)
	TLS           bool   `yaml:"tls,omitempty"`           // 监听器启用 TLS
	CertFile      string `yaml:"certFile,omitempty"`      // TLS 证书(tls: true 时必填)
	KeyFile       string `yaml:"keyFile,omitempty"`       // TLS 私钥

	// forward 插件:端口转发目标地址
	Target string `yaml:"target,omitempty"`
	// 入站协议转化(proxy 监听器):raw(默认)/ws,见 protocol/codec
	InCodec string `yaml:"inCodec,omitempty"`

	Connect     []ConnectItem `yaml:"connect,omitempty"`
	ClientProxy []ClientProxy `yaml:"clientProxy,omitempty"`
}

type ConnectItem struct {
	ProxyName       string `yaml:"proxyName" json:"proxyName"`
	BridgeName      string `yaml:"bridgeName,omitempty" json:"bridgeName,omitempty"`
	ClientProxyName string `yaml:"clientProxyName" json:"clientProxyName"`
	StreamId        string `yaml:"streamId" json:"streamId"`
}

type ClientProxy struct {
	ClientProxyName string `yaml:"clientProxyName" json:"clientProxyName"`
	StreamId        string `yaml:"streamId" json:"streamId"`
	Addr            string `yaml:"addr" json:"addr"`
	Transport       string `yaml:"transport" json:"transport,omitempty"`
	KeyPassword     string `yaml:"keyPassword,omitempty" json:"keyPassword,omitempty"`
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
