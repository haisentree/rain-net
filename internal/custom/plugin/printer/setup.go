package printer

import (
	"rain-net/internal/custom/customserver"
	"rain-net/internal/custom/plugin"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("printer", setup)
}

func setup(c *pluginer.Controller) error {
	p := &Printer{}
	customserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
