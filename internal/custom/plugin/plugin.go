package plugin

import (
	"context"
	"rain-net/protocol/custom"

	"github.com/miekg/dns"
)

type (
	Plugin func(Handler) Handler

	Handler interface {
		ServeCustom(context.Context, custom.ResponseWriter, *custom.Msg) error
		Name() string
	}
	// 用于测试使用的
	HandlerFunc func(context.Context, dns.ResponseWriter, *dns.Msg) (int, error)
)

func (f HandlerFunc) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	return f(ctx, w, r)
}

func (f HandlerFunc) Name() string { return "handlerfunc" }
