package main

import (
	"rain-net/internal/star/starserver"
	_ "rain-net/internal/star/zplugin"
)

func main() {
	starserver.Run()
}
