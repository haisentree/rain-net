package writer

import (
	"context"
	"rain-net/internal/custom/plugin"
)

type Writer struct {
	Next plugin.Handler
}

func (w Writer) ServeCustom(ctx context.Context) error {
	return nil
}

func (w Writer) Name() string { return "writer" }
