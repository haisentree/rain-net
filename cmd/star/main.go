package main

import (
	"github.com/haisentree/rain-net/internal/star/starserver"
	_ "github.com/haisentree/rain-net/internal/star/zplugin"
)

func main() {
	starserver.Run()
}
