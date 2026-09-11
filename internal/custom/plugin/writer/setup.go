package writer

import (
	"github.com/haisentree/rain-net/internal/custom/customserver"
	"github.com/haisentree/rain-net/internal/custom/plugin"
	"github.com/haisentree/rain-net/pluginer"
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
