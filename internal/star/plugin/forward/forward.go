package forward

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"rain-net/internal/star/plugin"
	"rain-net/internal/star/starserver"
	"rain-net/protocol/star"
)

// Forward TCP 端口转发插件:把监听器收到的连接原样转发到 settings.target
//
// 配置示例:
//
//	listenerList:
//	  - name: listener-forward
//	    type: tcp
//	    transport: tcp
//	    addr: 0.0.0.0:5180
//	    settings:
//	      target: 127.0.0.1:9991   # 转发目标
//	handlerList:
//	  - name: handler-forward
//	    plugins: [forward]
type Forward struct {
	Next   plugin.Handler
	Target string
}

func (p *Forward) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	w.SetKeepAlive(true)

	target, err := net.DialTimeout("tcp", p.Target, 5*time.Second)
	if err != nil {
		w.SetKeepAlive(false)
		return fmt.Errorf("forward: dial target %s: %w", p.Target, err)
	}
	defer target.Close()

	// 双向搬运,任一方向结束即收尾(半关闭对端)
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(target, w.GetReader())
		done <- err
	}()
	go func() {
		_, err := io.Copy(w, target)
		done <- err
	}()

	if err := <-done; err != nil {
		slog.Debug("forward copy ended", "target", p.Target, "err", err)
	}
	w.SetKeepAlive(false)
	return nil
}

func (p *Forward) Name() string { return "forward" }
