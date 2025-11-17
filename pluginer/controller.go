package pluginer

type Controller struct {
	instance *Instance
}

func (c *Controller) ServerType() string {
	return c.instance.serverType
}
