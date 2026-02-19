package httPproxy

import (
	"context"
	"fmt"
	"rain-net/internal/star/plugin"
	"rain-net/protocol/star"
)

type HttpProxy struct {
	Next plugin.Handler
}

func (p HttpProxy) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	fmt.Println("star HttpProxy")
	if p.Next != nil {
		p.Next.ServeStar(ctx, w, data)
	}
	return nil
}

func (p HttpProxy) Name() string { return "HttpProxy" }
