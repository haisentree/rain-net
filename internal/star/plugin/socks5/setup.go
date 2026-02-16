package socks5

import (
	"rain-net/internal/star/plugin"
	"rain-net/internal/star/starserver"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("socks5", setup)
}

func setup(c *pluginer.Controller) error {
	p := &Socks5{}
	starserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
