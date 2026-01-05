package main

import (
	"rain-net/internal/custom/customserver"
	_ "rain-net/internal/custom/zplugin"
)

func main() {
	customserver.Run()
}
