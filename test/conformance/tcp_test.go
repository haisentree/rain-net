package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/tcp"
)

func TestConformanceTCP(t *testing.T) {
	tr, err := transport.Build("tcp", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runSuite(t, "tcp", tr)
}
