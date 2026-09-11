package e2e

// 测试二进制的传输注册:与 cmd/raind 的 zplugin 等价
import (
	_ "github.com/haisentree/rain-net/internal/transport/h2"
	_ "github.com/haisentree/rain-net/internal/transport/quic"
	_ "github.com/haisentree/rain-net/internal/transport/tcp"
	_ "github.com/haisentree/rain-net/internal/transport/tls"
	_ "github.com/haisentree/rain-net/internal/transport/ws"
)
