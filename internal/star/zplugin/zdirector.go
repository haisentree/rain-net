// Package zplugin 汇总 star 服务端二进制的全部插件注册(空白导入)。
//
// 协议 handler 插件与 transport 传输插件都在此挂载:
// 新增一种插件/传输 = 新增一行空白导入,装配层零改动。
package zplugin

import (
	_ "github.com/haisentree/rain-net/internal/star/plugin/forward"
	_ "github.com/haisentree/rain-net/internal/star/plugin/httpProxy"
	_ "github.com/haisentree/rain-net/internal/star/plugin/printer"
	_ "github.com/haisentree/rain-net/internal/star/plugin/socks5"
	_ "github.com/haisentree/rain-net/internal/transport/h2"
	_ "github.com/haisentree/rain-net/internal/transport/quic"
	_ "github.com/haisentree/rain-net/internal/transport/tcp"
	_ "github.com/haisentree/rain-net/internal/transport/tls"
	_ "github.com/haisentree/rain-net/internal/transport/ws"
)
