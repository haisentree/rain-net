package pluginer

// 舍弃

type Controller struct {
	instance *Instance
}

func (c *Controller) ServerType() string {
	return c.instance.serverType
}

func (c *Controller) OnFirstStartup(fn func() error) {
	c.instance.OnFirstStartup = append(c.instance.OnFirstStartup, fn)
}

func (c *Controller) OnStartup(fn func() error) {
	c.instance.OnStartup = append(c.instance.OnStartup, fn)
}

func (c *Controller) OnRestart(fn func() error) {
	c.instance.OnRestart = append(c.instance.OnRestart, fn)
}

func (c *Controller) OnRestartFailed(fn func() error) {
	c.instance.OnRestartFailed = append(c.instance.OnRestartFailed, fn)
}

func (c *Controller) OnShutdown(fn func() error) {
	c.instance.OnShutdown = append(c.instance.OnShutdown, fn)
}

func (c *Controller) OnFinalShutdown(fn func() error) {
	c.instance.OnFinalShutdown = append(c.instance.OnFinalShutdown, fn)
}
