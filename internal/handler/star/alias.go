// Package starhandler 承载 star 隧道的服务端协议面:
// ctrl 监听器(握手/注册/分发)、共享中枢(CtrlHub/StreamTable)。
// 从 starserver 拆出(docs/design.md §五 迁移映射);admin/proxy 拆迁见后续切片。
package starhandler

import (
	"github.com/haisentree/rain-net/config"
)

type (
	ListenerList = config.ListenerList
	Settings     = config.Settings
	ClientProxy  = config.ClientProxy
	ConnectItem  = config.ConnectItem
)
