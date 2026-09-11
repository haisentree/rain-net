package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/h2"
)

func TestConformanceH2(t *testing.T) {
	tr, err := transport.Build("h2", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// 半关闭契约由拨号(客户端)侧支持:CloseWrite = 请求体 END_STREAM
	runSuite(t, "h2", tr)
}
