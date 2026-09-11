package main

import (
	"github.com/haisentree/rain-net/internal/custom/customserver"
	_ "github.com/haisentree/rain-net/internal/custom/zplugin"
)

func main() {
	customserver.Run()
}
