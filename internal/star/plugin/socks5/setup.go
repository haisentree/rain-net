package socks5

import (
	"log"
	"os"
	"github.com/haisentree/rain-net/internal/star/plugin"
	"github.com/haisentree/rain-net/internal/star/starserver"
	"github.com/haisentree/rain-net/pluginer"
)

func init() {
	plugin.Register("socks5", setup)
}

func setup(c *pluginer.Controller) error {
	conf := &Config{
		AuthMethods: []Authenticator{},
		Logger:      log.New(os.Stdout, "", log.LstdFlags),
	}
	serv, err := New(conf)
	if err != nil {
		return err
	}

	p := &Socks5{
		Server: serv,
	}
	starserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		p.Next = next
		return p
	})

	return nil
}
