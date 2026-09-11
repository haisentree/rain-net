package config

import (
	"strings"
	"testing"
)

func TestLoadNewSchema(t *testing.T) {
	data := []byte(`
service:
  - name: gw
    ingress:
      - name: socks-in
        transport: tcp
        addr: 0.0.0.0:8085
        handler: proxy-chain
      - name: star-in
        transport: tls
        addr: 0.0.0.0:5172
        handler: star
        settings:
          credentials:
            - name: client-0
              keyPassword: pwd-0
              streams: [svc-a, svc-b]
      - name: admin-in
        transport: tcp
        addr: 127.0.0.1:5173
        handler: admin
        settings: { adminPassword: secret }

handlers:
  - name: proxy-chain
    plugins: [socks5]

dialers:
  - name: office
    type: star
    transport: tls
    addr: 127.0.0.1:5172
    identity: client-0
    keyPassword: pwd-0
    locals:
      - stream: svc-a
        addr: 127.0.0.1:9991
  - name: lan
    type: direct

routes:
  - ingress: socks-in
    to: office
    streams: [svc-a, svc-b]
`)
	root, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Services) != 1 || len(root.Services[0].Ingress) != 3 {
		t.Fatalf("services = %+v", root.Services)
	}
	if len(root.Handlers) != 1 || root.Handlers[0].Plugins[0] != "socks5" {
		t.Fatalf("handlers = %+v", root.Handlers)
	}
	if len(root.Dialers) != 2 || root.Dialers[0].Identity != "client-0" {
		t.Fatalf("dialers = %+v", root.Dialers)
	}
	if len(root.Routes) != 1 || len(root.Routes[0].Streams) != 2 {
		t.Fatalf("routes = %+v", root.Routes)
	}
}

// TestLoadRejectsLegacySchema 旧 schema 直接报错并附迁移对照(§十.2)
func TestLoadRejectsLegacySchema(t *testing.T) {
	data := []byte(`
listenerList:
  - name: listener-0
    type: tcp
    addr: 0.0.0.0:5170
dailerList:
  - name: dailer-0
    addr: 127.0.0.1:5172
handlerList:
  - name: handler-0
    plugins: [socks5]
`)
	_, err := Load(data)
	if err == nil {
		t.Fatal("expected legacy schema to be rejected")
	}
	for _, key := range []string{"listenerList", "dailerList", "handlerList", "dialers"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("error missing %q: %v", key, err)
		}
	}
}

func TestValidateRejectsBadDialer(t *testing.T) {
	data := []byte(`
dialers:
  - name: bad
    type: snail
`)
	if _, err := Load(data); err == nil {
		t.Fatal("expected invalid dialer type to be rejected")
	}

	data2 := []byte(`
dialers:
  - name: d0
    type: star
    addr: 127.0.0.1:1
    identity: client-0
    keyPassword: pwd
routes:
  - to: missing
`)
	if _, err := Load(data2); err == nil || !strings.Contains(err.Error(), "未定义的 dialer") {
		t.Fatalf("route referencing undefined dialer should fail, got %v", err)
	}
}
