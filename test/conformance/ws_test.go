package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/ws"
)

func TestConformanceWS(t *testing.T) {
	tr, err := transport.Build("ws", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// ws 无半关闭语义:走降级场景(design.md 半关闭矩阵 ws 列)
	runSuiteCaps(t, "ws", tr, Caps{HalfClose: false})
}
