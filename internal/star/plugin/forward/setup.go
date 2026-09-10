package forward

import (
	"errors"

	"rain-net/internal/star/plugin"
	"rain-net/internal/star/starserver"
	"rain-net/pluginer"
)

func init() {
	plugin.Register("forward", setup)
}

func setup(c *pluginer.Controller) error {
	settings := starserver.GetListenerSettings(c)
	if settings.Target == "" {
		return errors.New("forward: listener settings.target is required")
	}

	p := &Forward{Target: settings.Target}
	starserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
