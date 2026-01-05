package pluginer

type Configer interface {
	GetConfig() Config
}

type Config struct {
	Service []Service `yaml:"service"`
}

type Service struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Host     []Host `yaml:"host"`
}

type Host struct {
	Network string   `yaml:"network"`
	Address string   `yaml:"address"`
	Plugin  []string `yaml:"plugin"`
}
