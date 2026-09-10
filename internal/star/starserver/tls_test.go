package starserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rain-net/protocol/star"
)

// genSelfSignedCert 生成测试用自签名证书(SAN: localhost/127.0.0.1)
func genSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestCtrlTLS(t *testing.T) {
	certFile, keyFile := genSelfSignedCert(t)

	hub := NewCtrlHub()
	ctrl := NewCtrlProxyServer("svc", &ListenerList{
		Type:      "ctrlproxy",
		Transport: "tcp",
		Settings: Settings{
			TLS:      true,
			CertFile: certFile,
			KeyFile:  keyFile,
			ClientProxy: []ClientProxy{
				{ClientProxyName: "clientProxy-0", KeyPassword: "pwd-0"},
			},
		},
	}, hub)

	ln, err := ctrl.Listen()
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = ctrl.Serve(ln) }()

	// TLS 客户端:跳过自签名证书校验
	client, err := star.DialCtrlTLS("tcp", ln.Addr().String(),
		star.HandshakeReq{ClientName: "dailer-0", KeyPassword: "pwd-0"},
		[]star.RegisterStreamReq{{ClientProxyName: "clientProxy-0", StreamId: "stream-a"}},
		3*time.Second, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("DialCtrlTLS: %v", err)
	}
	defer client.Close()
	waitFor(t, 3*time.Second, "stream registered over tls", func() bool {
		return hub.Streams.Len() == 1
	})

	// 明文客户端拨 TLS 端口:握手应失败
	if _, err := star.DialCtrl("tcp", ln.Addr().String(),
		star.HandshakeReq{KeyPassword: "pwd-0"}, nil, 3*time.Second); err == nil {
		t.Fatal("expected plain dial against TLS listener to fail")
	}
}
