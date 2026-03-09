package internal

type CtrlProxy struct {
	Name        string
	Connect     []ConnectProxy
	ClientProxy []ClientProxy
}

type ConnectProxy struct {
	ProxyName       string
	BridgeName      string
	ClientProxyName string
	StreamId        string
}

type ClientProxy struct {
	ClientProxyName string
	StreamId        string
	Addr            string
	Transport       string
	KeyPassword     string
}

func NewCtrlProxy(name string, connect []ConnectProxy, clientProxy []ClientProxy) *CtrlProxy {
	// 将连接实体注册到bridge,CtrlProxy管理连接的配置信息

	// 1.复制配置信息

	// 2.执行配置

	return &CtrlProxy{
		Name:        name,
		Connect:     connect,
		ClientProxy: clientProxy,
	}
}

// 页面操作,先创建proxy,再添加connect和clientProxy
func (c *CtrlProxy) AddConnect(streamId string) error {
	return nil
}

func (c *CtrlProxy) AddClientProxy(clientProxy ClientProxy) error {
	return nil
}
