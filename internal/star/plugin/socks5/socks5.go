package socks5

import (
	"context"
	"github.com/haisentree/rain-net/internal/star/plugin"
	"github.com/haisentree/rain-net/protocol/star"
)

type Socks5 struct {
	Next   plugin.Handler
	Server *Server
}

func (p Socks5) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	w.SetKeepAlive(true)

	err := p.Server.ServeHandle(w.GetConn(), w.GetReader())
	if err != nil {
		w.SetKeepAlive(false)
		return err
	}

	return nil
}

func (p Socks5) Name() string { return "socks5" }
