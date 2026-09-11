package writer

import (
	"context"
	"fmt"
	"github.com/haisentree/rain-net/internal/custom/plugin"
	"github.com/haisentree/rain-net/protocol/custom"
)

type Writer struct {
	Next plugin.Handler
}

func (w Writer) ServeCustom(ctx context.Context, resp custom.ResponseWriter, msg *custom.Msg) error {
	fmt.Println("custom printer")
	return nil
}

func (w Writer) Name() string { return "writer" }
