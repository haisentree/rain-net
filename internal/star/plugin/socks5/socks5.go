package socks5

import (
	"context"
	"fmt"
	"rain-net/internal/star/plugin"
	"rain-net/protocol/star"
)

type Socks5 struct {
	Next plugin.Handler
}

func (p Socks5) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	fmt.Println("star Socks5")
	return nil
}

func (p Socks5) Name() string { return "socks5" }
