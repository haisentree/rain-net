package plugin

import (
	"context"
	"rain-net/protocol/star"
)

type (
	Plugin func(Handler) Handler

	Handler interface {
		ServeStar(context.Context, star.ResponseWriter, []byte) error
		Name() string
	}

	HandlerFunc func(context.Context, []byte) (int, error)
)

func (f HandlerFunc) ServeStar(ctx context.Context, data []byte) (int, error) {
	return f(ctx, data)
}

func (f HandlerFunc) Name() string { return "handlerfunc" }
