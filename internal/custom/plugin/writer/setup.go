package writer

import (
	"rain-net/internal/custom/customserver"
	"rain-net/internal/custom/plugin"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("writer", setup)
}

func setup(c *pluginer.Controller) error {
	p := &Writer{}
	customserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})
	return nil
}
