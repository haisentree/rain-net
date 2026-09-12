package starserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haisentree/rain-net/internal/handler/dns"
	"github.com/haisentree/rain-net/pluginer"
)

// writeConfig 写一版配置(dns ingress,端口 0,便于多实例共存)。
func writeConfig(t *testing.T, path, wwwIP string) {
	t.Helper()
	cfg := `service:
  - name: n
    ingress:
      - name: dns-in
        transport: tcp
        addr: 127.0.0.1:0
        handler: dns
        settings:
          dnsZones:
            example.org:
              www: ` + wwwIP + `
`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// findDNS 从实例的服务组件中找 dns 处理器。
func findDNS(inst *pluginer.Instance) *dns.Handler {
	for _, sl := range inst.Servers() {
		if h, ok := sl.Server().(*dns.Handler); ok {
			return h
		}
	}
	return nil
}

// TestReload 热更新安全语义:坏配置保持旧实例;新配置生效到新实例。
func TestReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, cfgPath, "10.0.0.1")

	input := pluginer.YAMLFileInput{Filepath: cfgPath, ServerTypeName: "star"}
	inst, err := pluginer.Start(input)
	if err != nil {
		t.Fatal(err)
	}
	h1 := findDNS(inst)
	if h1 == nil {
		t.Fatal("dns handler not found in initial instance")
	}
	if h1.Zones()["example.org"]["www"] != "10.0.0.1" {
		t.Fatalf("initial zone = %v", h1.Zones())
	}

	// 1) 旧 schema 配置:拒绝,旧实例原样保留
	if err := os.WriteFile(cfgPath, []byte("listenerList: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, rerr := Reload(inst, input)
	if rerr == nil {
		t.Fatal("expected legacy schema to be rejected on reload")
	}
	if old != inst {
		t.Fatal("instance should be unchanged after failed reload")
	}
	if h1.Zones()["example.org"]["www"] != "10.0.0.1" {
		t.Fatalf("zone changed after failed reload: %v", h1.Zones())
	}

	// 2) 合法新配置:新实例生效新 zone
	writeConfig(t, cfgPath, "10.0.0.9")
	inst2, rerr := Reload(inst, input)
	if rerr != nil {
		t.Fatal(rerr)
	}
	h2 := findDNS(inst2)
	if h2 == nil {
		t.Fatal("dns handler not found in reloaded instance")
	}
	if got := h2.Zones()["example.org"]["www"]; got != "10.0.0.9" {
		t.Fatalf("reloaded zone = %q, want 10.0.0.9", got)
	}
	inst2.Stop()
}
