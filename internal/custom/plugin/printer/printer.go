package printer

import "context"

type Printer struct{}

func (p Printer) ServeCustom(ctx context.Context) error {
	return nil
}
