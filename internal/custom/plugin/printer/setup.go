package printer

import (
	"rain-net/internal/custom/plugin"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("printer", setup)
}

func setup(c *pluginer.Controller) error {
	return nil
}
