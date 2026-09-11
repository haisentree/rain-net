// Package config 定义各服务的配置模型(YAML schema)。
//
// 本包只放纯数据结构与校验,不含运行时状态;R3 新 schema(ingress/dialers/routes)
// 切换后,旧字段名随兼容策略废弃(docs/design.md §十.2:旧 schema 直接报错 + 迁移提示)。
package config

// Service 一个服务实例:绑定若干监听器与拨号器
type Service struct {
	Name      string      `yaml:"name"`
	Type      string      `yaml:"type"`
	Listeners []Listeners `yaml:"listeners"`
	Dailers   []string    `yaml:"dailers"`
}

// Listeners 服务与监听器/处理器链的绑定关系
type Listeners struct {
	ListenerName string `yaml:"listenerName"`
	HandlerName  string `yaml:"handlerName"`
}

// ListenerList 监听器定义
type ListenerList struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	Transport string `yaml:"transport,omitempty"`
	Addr      string `yaml:"addr"`

	Settings Settings `yaml:"settings"`
}

// HandlerList 处理器链定义
type HandlerList struct {
	Name    string   `yaml:"name"`
	Plugins []string `yaml:"plugins"`
}

// DailerList 拨号器(隧道客户端)定义
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

// Settings 监听器设置
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

// ConnectItem connect 路由规则
type ConnectItem struct {
	ProxyName       string `yaml:"proxyName" json:"proxyName"`
	BridgeName      string `yaml:"bridgeName,omitempty" json:"bridgeName,omitempty"`
	ClientProxyName string `yaml:"clientProxyName" json:"clientProxyName"`
	StreamId        string `yaml:"streamId" json:"streamId"`
}

// ClientProxy 客户端凭据/流定义
type ClientProxy struct {
	ClientProxyName string `yaml:"clientProxyName" json:"clientProxyName"`
	StreamId        string `yaml:"streamId" json:"streamId"`
	Addr            string `yaml:"addr" json:"addr"`
	Transport       string `yaml:"transport" json:"transport,omitempty"`
	KeyPassword     string `yaml:"keyPassword,omitempty" json:"keyPassword,omitempty"`
}
