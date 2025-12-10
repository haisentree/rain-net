package customserver

// 协议特有的配置
// TODO:配置动态监听,使用uuid,如果变化了定向重启
type Config struct {
	Service []Service `yaml:"service"`
}

type Service struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Host     []Host `yaml:"host"`
}

type Host struct {
	Key     string `yaml:"key"`
	Network string `yaml:"network"`
	Address string `yaml:"address"`
}
