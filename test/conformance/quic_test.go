package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/quic"
)

func TestConformanceQUIC(t *testing.T) {
	certFile, keyFile := genCert(t)
	tr, err := transport.Build("quic", transport.Options{
		CertFile:      certFile,
		KeyFile:       keyFile,
		TLSSkipVerify: true, // 测试自签名证书
	})
	if err != nil {
		t.Fatal(err)
	}
	// QUIC 原生支持半关闭:全场景完整断言
	runSuite(t, "quic", tr)
}
