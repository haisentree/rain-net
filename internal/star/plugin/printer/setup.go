package printer

import (
	"github.com/haisentree/rain-net/internal/star/plugin"
	"github.com/haisentree/rain-net/internal/star/starserver"
	"github.com/haisentree/rain-net/pluginer"
)

func init() {
	plugin.Register("printer", setup)
}

func setup(c *pluginer.Controller) error {
	p := &Printer{}
	starserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
