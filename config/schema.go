package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// 新 schema(design.md §三):协议 × 传输正交,路由选向。
// service[].ingress 为入站接入点;handlers 为处理器链;dialers 为出站;
// routes 按声明顺序首中即停。

// Root 配置根。
type Root struct {
	Services []ServiceSpec `yaml:"service"`
	Handlers []HandlerSpec `yaml:"handlers"`
	Dialers  []DialerSpec  `yaml:"dialers"`
	Routes   []RouteSpec   `yaml:"routes"`
}

// ServiceSpec 一个服务实例。
type ServiceSpec struct {
	Name    string        `yaml:"name"`
	Ingress []IngressSpec `yaml:"ingress"`
	Dialers []string      `yaml:"dialers,omitempty"` // 本服务引用的拨号器名(声明性,暂作提示)
}

// IngressSpec 入站接入点(角色,非接口——design.md §三 命名决策)。
type IngressSpec struct {
	Name      string       `yaml:"name"`
	Transport string       `yaml:"transport,omitempty"` // tcp|tls|ws|h2|quic(经 transport 注册表)
	Addr      string       `yaml:"addr"`
	Handler   string       `yaml:"handler,omitempty"` // 处理器链名;内置:star / admin;空 = 连接泵代理
	Settings  IngressExtra `yaml:"settings,omitempty"`
}

// IngressExtra ingress 的 settings:内置 handler 各取所需,插件可经
// GetListenerSettings 读取(多候选 key 机制后续统一)。
type IngressExtra struct {
	AdminPassword string       `yaml:"adminPassword,omitempty"`
	KeyPassword   string       `yaml:"keyPassword,omitempty"` // star handler:管理员密码(不限身份)
	Credentials   []Credential `yaml:"credentials,omitempty"` // star handler:客户端凭据表

	Target  string `yaml:"target,omitempty"`  // forward 插件:转发目标
	InCodec string `yaml:"inCodec,omitempty"` // 入站协议转化:raw(默认)/ws

	CertFile string `yaml:"certFile,omitempty"`
	KeyFile  string `yaml:"keyFile,omitempty"`
}

// Credential star 隧道客户端凭据:一个身份可注册多个流。
type Credential struct {
	Name        string   `yaml:"name"`
	KeyPassword string   `yaml:"keyPassword"`
	Streams     []string `yaml:"streams,omitempty"`
}

// HandlerSpec 处理器链定义(协议语义层,pluginer 插件序列)。
type HandlerSpec struct {
	Name    string   `yaml:"name"`
	Plugins []string `yaml:"plugins"`
}

// DialerSpec 出站定义:direct(直连)或 star(隧道)。
type DialerSpec struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"` // direct|star
	Transport   string      `yaml:"transport,omitempty"`
	Addr        string      `yaml:"addr,omitempty"`
	Identity    string      `yaml:"identity,omitempty"`    // star:客户端身份(clientProxyName)
	KeyPassword string      `yaml:"keyPassword,omitempty"` // star:接入凭据
	Locals      []LocalSpec `yaml:"locals,omitempty"`      // star:stream → 本地内网服务地址
}

// LocalSpec rainc 侧 stream 到本地服务的映射。
type LocalSpec struct {
	Stream string `yaml:"stream"`
	Addr   string `yaml:"addr"`
}

// RouteSpec 路由规则:匹配 (ingress, identity, target),命中后交给 dialer。
// 声明顺序即优先级,首中即停(§九.5)。
type RouteSpec struct {
	Ingress  string   `yaml:"ingress,omitempty"`
	Identity string   `yaml:"identity,omitempty"`
	Target   string   `yaml:"target,omitempty"`
	To       string   `yaml:"to"`
	Streams  []string `yaml:"streams,omitempty"`
}

// 旧 schema 字段 → 新字段迁移对照(§十.2:检测到即报错,零兼容代码)
var legacyKeys = map[string]string{
	"listenerList": "service[].ingress(listenerList → service[].ingress)",
	"handlerList":  "handlers(handlerList → handlers)",
	"dailerList":   "dialers(dailerList → dialers,注意拼写修正)",
	"bridgerList":  "bridges(bridgerList → bridges,M5)",
}

// Load 解析并校验新 schema 配置;检测到旧 schema 字段时报错并附迁移对照。
func Load(data []byte) (*Root, error) {
	var probe map[string]any
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("config: yaml 解析失败: %w", err)
	}
	var legacy []string
	for key, hint := range legacyKeys {
		if _, ok := probe[key]; ok {
			legacy = append(legacy, fmt.Sprintf("  %s → %s", key, hint))
		}
	}
	if len(legacy) > 0 {
		sortStrings(legacy)
		return nil, fmt.Errorf(
			"config: 检测到旧 schema 字段,请按以下对照迁移后重试(旧 schema 已不再支持):\n%s",
			strings.Join(legacy, "\n"))
	}

	root := &Root{}
	if err := yaml.Unmarshal(data, root); err != nil {
		return nil, fmt.Errorf("config: 解析失败: %w", err)
	}
	if err := root.validate(); err != nil {
		return nil, err
	}
	return root, nil
}

func (r *Root) validate() error {
	names := map[string]bool{}
	for _, svc := range r.Services {
		if svc.Name == "" {
			return fmt.Errorf("config: service.name 必填")
		}
		for _, ing := range svc.Ingress {
			if ing.Name == "" || ing.Addr == "" {
				return fmt.Errorf("config: ingress.name/addr 必填(service %s)", svc.Name)
			}
			if names["ingress:"+ing.Name] {
				return fmt.Errorf("config: ingress %q 重名", ing.Name)
			}
			names["ingress:"+ing.Name] = true
		}
	}
	for _, h := range r.Handlers {
		if names["handler:"+h.Name] {
			return fmt.Errorf("config: handler %q 重名", h.Name)
		}
		names["handler:"+h.Name] = true
	}
	for _, d := range r.Dialers {
		if d.Type != "direct" && d.Type != "star" {
			return fmt.Errorf("config: dialer %s.type 必须为 direct 或 star", d.Name)
		}
		if d.Type == "star" && (d.Addr == "" || d.Identity == "" || d.KeyPassword == "") {
			return fmt.Errorf("config: star dialer %s 需要 addr/identity/keyPassword", d.Name)
		}
		if names["dialer:"+d.Name] {
			return fmt.Errorf("config: dialer %q 重名", d.Name)
		}
		names["dialer:"+d.Name] = true
	}
	dialerByName := map[string]DialerSpec{}
	for _, d := range r.Dialers {
		dialerByName[d.Name] = d
	}
	for _, rt := range r.Routes {
		if _, ok := dialerByName[rt.To]; !ok {
			return fmt.Errorf("config: route(to=%s) 引用了未定义的 dialer", rt.To)
		}
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
