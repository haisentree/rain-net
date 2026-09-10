package starserver

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"rain-net/pluginer"

	"gopkg.in/yaml.v3"
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

// 初始化特定服务的Config
func newContext(i *pluginer.Instance) pluginer.Context {
	data, err := os.ReadFile(newDefaultInput().Path())
	if err != nil {
		panic(err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		panic(err)
	}

	config.ListenerMap = make(map[string]*ListenerList, len(config.ListenerList))
	config.HandlerMap = make(map[string]HandlerList, len(config.HandlerList))
	config.DailerMap = make(map[string]DailerList, len(config.DailerList))

	// 监听器以指针入表:admin 等组件对 settings 的运行时修改全局可见
	for i := range config.ListenerList {
		config.ListenerMap[config.ListenerList[i].Name] = &config.ListenerList[i]
	}
	for _, val := range config.HandlerList {
		config.HandlerMap[val.Name] = val
	}
	for _, val := range config.DailerList {
		config.DailerMap[val.Name] = val
	}

	return &starContext{
		Configs:       &config,
		ZoneToConfigs: make(map[string]*Config),
	}
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
	hub := NewCtrlHub()

	// 路由规则表:配置文件静态规则做种子,运行时可经 admin API 动态增删
	connectTable := NewConnectTable()
	var staticRules []ConnectItem
	for _, l := range h.Configs.ListenerList {
		staticRules = append(staticRules, l.Settings.Connect...)
	}
	connectTable.Seed(staticRules)

	var ctrls []*CtrlProxyServer
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
				c := NewCtrlProxyServer(srv.Name, listener, hub)
				ctrls = append(ctrls, c)
				servers = append(servers, c)
				continue
			case "proxy":
				// bridger 暂未实现,同进程内通过 hub 直连
				p, err := NewProxyServer(srv.Name, listener, connectTable, hub)
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
			}

			switch listener.Transport {
			case "tcp":
				s, err := NewServer(srv.Name, listener.Transport, listener.Addr, h.ZoneToConfigs[key])
				if err != nil {
					slog.Warn("tcp NewServer", "err", err)
					continue
				}
				servers = append(servers, s)
			case "udp":
				s, err := NewServer(srv.Name, listener.Transport, listener.Addr, h.ZoneToConfigs[key])
				if err != nil {
					slog.Warn("udp NewServer", "err", err)
					continue
				}
				servers = append(servers, s)
			default:
				panic(fmt.Sprintf("unsupported transport: %s", listener.Transport))
			}
		}
	}

	for _, l := range adminListeners {
		servers = append(servers, NewAdminServer(l.Name, l, hub, connectTable, ctrls))
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
