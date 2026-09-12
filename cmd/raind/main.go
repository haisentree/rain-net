package main

import (
	"github.com/haisentree/rain-net/internal/star/starserver"
	_ "github.com/haisentree/rain-net/internal/star/zplugin"
)

// raind 服务端入口:SIGHUP 触发热更新,SIGINT/SIGTERM 优雅退出(见 starserver.Run)。
func main() {
	starserver.Run()
}
