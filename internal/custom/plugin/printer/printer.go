package printer

import (
	"context"
	"fmt"
	"rain-net/internal/custom/plugin"
)

type Printer struct {
	Next plugin.Handler
}

func (p Printer) ServeCustom(ctx context.Context) error {
	fmt.Println("custom printer")
	return nil
}

func (p Printer) Name() string { return "printer" }
