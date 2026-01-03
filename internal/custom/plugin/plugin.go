package plugin

import (
	"context"

	"github.com/miekg/dns"
)

type (
	Plugin func(Handler) Handler

	Handler interface {
		ServeCustom(context.Context) error
		Name() string
	}
	HandlerFunc func(context.Context, dns.ResponseWriter, *dns.Msg) (int, error)
)

func (f HandlerFunc) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	return f(ctx, w, r)
}

func (f HandlerFunc) Name() string { return "handlerfunc" }
