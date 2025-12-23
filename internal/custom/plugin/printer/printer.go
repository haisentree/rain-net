package printer

import (
	"context"
	"rain-net/internal/custom/plugin"
)

type Printer struct {
	Next plugin.Handler
}

func (p Printer) ServeCustom(ctx context.Context) error {
	return nil
}

func (p Printer) Name() string { return "printer" }
