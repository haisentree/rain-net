package starserver

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/internal/handler/dns"
	starhandler "github.com/haisentree/rain-net/internal/handler/star"
	"github.com/haisentree/rain-net/internal/transport"
	"github.com/haisentree/rain-net/pluginer"
)

const serverType = "star"

func init() {
	pluginer.RegisterServerType(serverType, pluginer.ServerType{
		Directives:   newDirectives,
		DefaultInput: newDefaultInput,
		NewContext:   newContext,
	})
}

func newDirectives() []string {
	return []string{"socks5"}
}

func newDefaultInput() pluginer.Input {
	return pluginer.YAMLFileInput{
		Filepath:       "etc/star.example.yaml",
		Contents:       []byte("default"),
		ServerTypeName: serverType,
	}
}

// newContext 解析配置(新 schema,经 config.Load)并映射为运行时组件参数。
// 映射规则(R3-4d,过渡形态,4e 收口):
//   - ingress.handler == "star"  → ctrlproxy(隧道服务端),credentials 展开为 clientProxy 清单
//   - ingress.handler == "admin" → admin
//   - ingress.handler == ""      → proxy(连接泵,由 routes 驱动)
//   - 其他 handler 名             → 插件链帧服务(handlers[] 定义插件序列)
//   - routes 按拨号器定义展开为 connect 规则(星型拨号器:to→identity)
func newContext(i *pluginer.Instance) pluginer.Context {
	data, err := os.ReadFile(i.Input().Path())
	if err != nil {
		panic(err)
	}

	root, err := config.Load(data)
	if err != nil {
		panic(err)
	}

	return &starContext{
		Configs:       buildRuntimeConfig(root),
		ZoneToConfigs: make(map[string]*Config),
	}
}

var _ pluginer.Context = &starContext{}

// buildRuntimeConfig 新 schema → 运行时组件参数(旧内部形态,4e 收口后退役)
func buildRuntimeConfig(root *config.Root) *Config {
	cfg := &Config{
		ListenerMap: make(map[string]*ListenerList),
		HandlerMap:  make(map[string]HandlerList),
		DailerMap:   make(map[string]DailerList),
		DialerSpecs: make(map[string]config.DialerSpec),
	}
	for _, d := range root.Dialers {
		cfg.DialerSpecs[d.Name] = d
	}

	for _, h := range root.Handlers {
		cfg.HandlerMap[h.Name] = HandlerList{Name: h.Name, Plugins: h.Plugins}
		cfg.HandlerList = append(cfg.HandlerList, HandlerList{Name: h.Name, Plugins: h.Plugins})
	}

	dialerByName := make(map[string]config.DialerSpec, len(root.Dialers))
	for _, d := range root.Dialers {
		dialerByName[d.Name] = d
	}

	for _, svc := range root.Services {
		s := Service{Name: svc.Name, Type: serverType}
		for _, ing := range svc.Ingress {
			tr := ing.Transport
			if tr == "" {
				tr = "tcp"
			}
			ll := ListenerList{
				Name:      ing.Name,
				Transport: tr,
				Addr:      ing.Addr,
				Settings: Settings{
					AdminPassword: ing.Settings.AdminPassword,
					KeyPassword:   ing.Settings.KeyPassword,
					CertFile:      ing.Settings.CertFile,
					KeyFile:       ing.Settings.KeyFile,
					Target:        ing.Settings.Target,
					InCodec:       ing.Settings.InCodec,
					DNSZones:      ing.Settings.DNSZones,
					DNSUpstreams:  ing.Settings.DNSUpstreams,
				},
			}
			switch ing.Handler {
			case "star":
				ll.Type = "ctrlproxy"
				for _, cred := range ing.Settings.Credentials {
					for _, stream := range cred.Streams {
						ll.Settings.ClientProxy = append(ll.Settings.ClientProxy, ClientProxy{
							ClientProxyName: cred.Name,
							StreamId:        stream,
							KeyPassword:     cred.KeyPassword,
						})
					}
				}
			case "admin":
				ll.Type = "admin"
			case "dns":
				ll.Type = "dns"
			case "":
				// 空处理器 = 连接泵代理,路由按 ingress 名匹配
				ll.Type = "proxy"
				ll.Settings.ProxyName = ing.Name
			default:
				ll.Type = "tcp"
			}
			cfg.ListenerList = append(cfg.ListenerList, ll)
			cfg.ListenerMap[ll.Name] = &cfg.ListenerList[len(cfg.ListenerList)-1]
			s.Listeners = append(s.Listeners, Listeners{ListenerName: ing.Name, HandlerName: ing.Handler})
		}
		cfg.Service = append(cfg.Service, s)
	}

	// routes 按 dialer 角色分流:
	//   inbound dialer → connect 规则(远端 rainc 拨入,经典隧道);
	//   outbound dialer → 出站链路路由(本节点内嵌客户端链出,场景 C)
	for _, rt := range root.Routes {
		d, ok := dialerByName[rt.To]
		if !ok {
			continue
		}
		if d.Type == "star" && d.Role == "outbound" {
			streams := rt.Streams
			if len(streams) == 0 {
				streams = []string{""}
			}
			for _, stream := range streams {
				cfg.ChainRoutes = append(cfg.ChainRoutes, ChainRoute{
					ProxyName: rt.Ingress, Dialer: d.Name,
					Identity: d.Identity, StreamId: stream,
				})
			}
			continue
		}
		if len(rt.Streams) == 0 {
			cfg.Routes = append(cfg.Routes, ConnectItem{
				ProxyName: rt.Ingress, ClientProxyName: d.Identity,
			})
			continue
		}
		for _, stream := range rt.Streams {
			cfg.Routes = append(cfg.Routes, ConnectItem{
				ProxyName: rt.Ingress, ClientProxyName: d.Identity, StreamId: stream,
			})
		}
	}
	return cfg
}

var _ pluginer.Context = &starContext{}

type starContext struct {
	Configs       *Config
	ZoneToConfigs map[string]*Config
}

func (h *starContext) MakeServers() ([]pluginer.Server, error) {
	if len(h.Configs.Service) == 0 {
		return nil, errors.New("service is empty")
	}

	for zone, cfg := range h.ZoneToConfigs {
		slog.Debug("zone config", "zone", zone, "plugins", len(cfg.Plugin))
	}

	servers, err := h.makeServersForGroup(h.Configs.Service)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

// func (h *starContext) GetConfig() pluginer.Config {
// 	return h.Configs.GetConfig()
// }

func (h *starContext) GetConfig() pluginer.Config {
	targetConfig := pluginer.Config{
		Service: make([]pluginer.Service, 0, len(h.Configs.Service)),
	}

	ListenerMap := make(map[string]ListenerList, len(h.Configs.ListenerList))
	HandlerMap := make(map[string]HandlerList, len(h.Configs.HandlerList))

	for _, val := range h.Configs.ListenerList {
		ListenerMap[val.Name] = val
	}
	for _, val := range h.Configs.HandlerList {
		HandlerMap[val.Name] = val
	}

	for _, srcSrv := range h.Configs.Service {

		targetSrv := pluginer.Service{
			Name:        srcSrv.Name,
			ServiceType: srcSrv.Type,
			Host:        make([]pluginer.Host, 0, len(srcSrv.Listeners)),
		}
		for _, val := range srcSrv.Listeners {
			targetHost := pluginer.Host{
				// Network: ListenerMap[val.ListenerName].Type,
				Network: ListenerMap[val.ListenerName].Transport,
				Address: ListenerMap[val.ListenerName].Addr,
				Plugin:  HandlerMap[val.HandlerName].Plugins,
			}

			targetSrv.Host = append(targetSrv.Host, targetHost)
		}

		targetConfig.Service = append(targetConfig.Service, targetSrv)
	}
	return targetConfig
}

// 根据特定服务的配置makeserver
func (h *starContext) makeServersForGroup(srvList []Service) ([]pluginer.Server, error) {
	var servers []pluginer.Server

	// ctrl 通道共享中枢:ctrlproxy 往里登记流/路由上行,proxy 从里取流/开连接
	hub := starhandler.NewCtrlHub()

	// 路由规则表:配置文件静态规则做种子,运行时可经 admin API 动态增删
	connectTable := NewConnectTable()
	// 路由种子:newContext 已把新 schema routes 按 dialer 展开为 connect 规则
	connectTable.Seed(h.Configs.Routes)

	var ctrls []*starhandler.CtrlProxyServer
	var apis []pluginer.APIProvider
	var adminListeners []*ListenerList

	for _, srv := range srvList {
		if srv.Type != serverType {
			continue
		}

		for _, ls := range srv.Listeners {
			listener := h.Configs.ListenerMap[ls.ListenerName]
			key := fmt.Sprintf("%s://%s", listener.Transport, listener.Addr)

			// 先按监听器类型分发,普通 tcp/udp 监听器走默认分支
			switch listener.Type {
			case "ctrlproxy":
				c := starhandler.NewCtrlProxyServer(srv.Name, listener, hub)
				ctrls = append(ctrls, c)
				servers = append(servers, c)
				continue
			case "proxy":
				// bridger 暂未实现,同进程内通过 hub 直连;
				// 出站链路(场景 C)路由经选项注入(内嵌客户端拨向远端节点)
				var chainRoutes []ChainRoute
				for _, cr := range h.Configs.ChainRoutes {
					if cr.ProxyName == listener.Name {
						chainRoutes = append(chainRoutes, cr)
					}
				}
				var popts []ProxyOption
				if len(chainRoutes) > 0 {
					specs := make(map[string]config.DialerSpec, len(chainRoutes))
					for _, cr := range chainRoutes {
						if spec, ok := h.Configs.DialerSpecs[cr.Dialer]; ok {
							specs[cr.Dialer] = spec
						}
					}
					popts = append(popts, WithChainRoutes(chainRoutes, specs))
				}
				p, err := NewProxyServer(srv.Name, listener, connectTable, hub, popts...)
				if err != nil {
					slog.Warn("proxy NewServer", "err", err)
					continue
				}
				servers = append(servers, p)
				continue
			case "admin":
				// admin 放到循环结束后创建,以引用组内全部 ctrlproxy
				adminListeners = append(adminListeners, listener)
				continue
			case "dns":
				println("MAKE: dns case hit")
				dh := dns.New(listener.Name, listener.Settings.DNSZones, listener.Settings.DNSUpstreams)
				servers = append(servers, dh)
				apis = append(apis, dh)
				continue
			}

			// udp 走 PacketConn 特例(ListenPacket),transport 层 v1 不建模 UDP 数据报;
			// 其余传输必须已在 transport 注册表注册(tcp/tls/…),装配层不硬编码
			tOpts := transport.Options{
				CertFile: listener.Settings.CertFile,
				KeyFile:  listener.Settings.KeyFile,
			}
			if listener.Transport != "udp" {
				if _, err := transport.Build(listener.Transport, tOpts); err != nil {
					return nil, err
				}
			}
			s, err := NewServer(srv.Name, listener.Transport, listener.Addr, h.ZoneToConfigs[key], tOpts)
			if err != nil {
				slog.Warn("NewServer", "transport", listener.Transport, "err", err)
				continue
			}
			servers = append(servers, s)
		}
	}

	for _, l := range adminListeners {
		servers = append(servers, NewAdminServer(l.Name, l, hub, connectTable, ctrls, apis...))
	}
	return servers, nil
}

func (h *starContext) makeListeners() ([]pluginer.Server, error) {
	return nil, nil
}

func (s *starContext) makeBridgers() (err error) {
	return nil
}

func (s *starContext) makeDailers() (err error) {
	return nil
}

// BuildServersFromRoot 从新 schema 构建全部服务组件(测试与装配层入口)。
func BuildServersFromRoot(root *config.Root) ([]pluginer.Server, error) {
	ctx := &starContext{
		Configs:       buildRuntimeConfig(root),
		ZoneToConfigs: make(map[string]*Config),
	}
	return ctx.MakeServers()
}
