package printer

import (
	"context"
	"fmt"
	"rain-net/internal/custom/plugin"
	"rain-net/protocol/custom"
)

type Printer struct {
	Next plugin.Handler
}

func (p Printer) ServeCustom(ctx context.Context, resp custom.ResponseWriter, msg *custom.Msg) error {
	fmt.Println("custom printer")
	return nil
}

func (p Printer) Name() string { return "printer" }
