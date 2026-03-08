package printer

import (
	"context"
	"fmt"
	"rain-net/internal/star/plugin"
	"rain-net/protocol/star"
)

type Printer struct {
	Next plugin.Handler
}

func (p Printer) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	w.SetKeepAlive(true)

	var buf [1024]byte

	reader := w.GetReader()
	n, err := reader.Read(buf[:])
	if err != nil {
		w.SetKeepAlive(false)
		return err
	}
	fmt.Printf("Received data: %s\n", string(buf[:n]))
	fmt.Println("star printer")
	return nil
}

func (p Printer) Name() string { return "printer" }
