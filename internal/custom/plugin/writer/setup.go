package writer

import (
	"rain-net/internal/custom/plugin"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("writer", setup)
}

func setup(c *pluginer.Controller) error {
	return nil
}
