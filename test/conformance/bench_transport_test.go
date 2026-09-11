package conformance

import (
	"testing"

	"github.com/haisentree/rain-net/internal/transport"
	_ "github.com/haisentree/rain-net/internal/transport/h2"
	_ "github.com/haisentree/rain-net/internal/transport/tcp"
	_ "github.com/haisentree/rain-net/internal/transport/tls"
	_ "github.com/haisentree/rain-net/internal/transport/ws"
)

const benchPayload = 1 << 20 // 1MB

func BenchmarkEcho1MB_TCP(t *testing.B) {
	tr, _ := transport.Build("tcp", transport.Options{})
	benchEcho(t, tr, benchPayload)
}

func BenchmarkEcho1MB_TLS(t *testing.B) {
	certFile, keyFile := genCert(t)
	tr, err := transport.Build("tls", transport.Options{CertFile: certFile, KeyFile: keyFile, TLSSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	benchEcho(t, tr, benchPayload)
}

func BenchmarkEcho1MB_WS(t *testing.B) {
	tr, err := transport.Build("ws", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	benchEcho(t, tr, benchPayload)
}

func BenchmarkEcho1MB_H2(t *testing.B) {
	tr, err := transport.Build("h2", transport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	benchEcho(t, tr, benchPayload)
}
