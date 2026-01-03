package writer

import (
	"context"
	"fmt"
	"rain-net/internal/custom/plugin"
)

type Writer struct {
	Next plugin.Handler
}

func (w Writer) ServeCustom(ctx context.Context) error {
	fmt.Println("custom printer")
	return nil
}

func (w Writer) Name() string { return "writer" }
