package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/kcp"
)

func TestConformanceKCP(t *testing.T) {
	tr, err := transport.Build("kcp", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// smux 流无半关闭语义:走降级场景
	runSuiteCaps(t, "kcp", tr, Caps{HalfClose: false})
}
