package httPproxy

import (
	"rain-net/internal/star/plugin"
	"rain-net/internal/star/starserver"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("httpProxy", setup)
}

func setup(c *pluginer.Controller) error {
	p := &HttpProxy{}
	starserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
