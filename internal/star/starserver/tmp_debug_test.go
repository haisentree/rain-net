package starserver

import (
	"testing"

	"github.com/haisentree/rain-net/config"
)

func TestTmpTypes(t *testing.T) {
	root := &config.Root{
		Services: []config.ServiceSpec{{
			Name: "dns-node",
			Ingress: []config.IngressSpec{
				{Name: "dns-in", Transport: "tcp", Addr: "127.0.0.1:0", Handler: "dns",
					Settings: config.IngressExtra{DNSZones: map[string]map[string]string{"example.org": {"www": "10.0.0.1"}}}},
				{Name: "admin-in", Transport: "tcp", Addr: "127.0.0.1:0", Handler: "admin",
					Settings: config.IngressExtra{AdminPassword: "secret"}},
			},
		}},
	}
	cfg := buildRuntimeConfig(root)
	for _, l := range cfg.ListenerList {
		t.Logf("listener %s type=%s transport=%s dnsZones=%v", l.Name, l.Type, l.Transport, l.Settings.DNSZones)
	}
}
