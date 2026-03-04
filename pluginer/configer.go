package pluginer

// 代理caddyfile结构的配置
type Configer interface {
	GetConfig() Config
}

type Config struct {
	Service []Service
}

type Service struct {
	Name        string
	ServiceType string
	Host        []Host
}

type Host struct {
	Network string
	Address string
	Plugin  []string
}
