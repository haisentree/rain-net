package starserver

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/haisentree/rain-net/config"
	"github.com/haisentree/rain-net/pluginer"
)

// Run 启动服务进程:SIGHUP 触发热更新(校验新配置 → 停旧实例 → 起新实例),
// SIGINT/SIGTERM 优雅退出。
func Run() {
	input := pluginer.YAMLFileInput{
		Filepath:       "etc/star.example.yaml",
		Contents:       []byte("star"),
		ServerTypeName: "star",
	}

	inst, err := pluginer.Start(input)
	if err != nil {
		panic(err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGHUP:
			newInst, rerr := Reload(inst, input)
			if rerr != nil {
				slog.Error("reload failed, keeping current instance", "err", rerr)
				continue
			}
			inst = newInst
			slog.Info("reload complete")
		default: // SIGINT / SIGTERM
			inst.Stop()
			return
		}
	}
}

// Reload 热更新:先校验新配置(§十.2 旧 schema 直接拒绝),再停旧实例、起新实例。
// 校验失败时旧实例原样保留(零兼容代码的热更新安全语义)。
func Reload(old *pluginer.Instance, input pluginer.Input) (*pluginer.Instance, error) {
	data, err := os.ReadFile(input.Path())
	if err != nil {
		return old, err
	}
	if _, err := config.Load(data); err != nil {
		return old, err
	}
	old.Stop()
	return pluginer.Start(input)
}
