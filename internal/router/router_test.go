package router

import (
	"testing"

	"github.com/haisentree/rain-net/pkg/rainnet"
)

func TestFirstHitOrder(t *testing.T) {
	r := New()
	r.Seed([]Rule{
		{Ingress: "socks-in", Dialer: "office"},
		{Dialer: "lan"},
	})

	// 命中第一条(声明顺序,首中即停)
	d, ok := r.Match(rainnet.Meta{Listener: "socks-in"})
	if !ok || d.Dialer != "office" {
		t.Fatalf("match = %+v %v, want office", d, ok)
	}
	// 不满足第一条的通配条件,落到第二条
	d, ok = r.Match(rainnet.Meta{Listener: "web-in"})
	if !ok || d.Dialer != "lan" {
		t.Fatalf("match = %+v %v, want lan", d, ok)
	}
}

func TestWildcardFields(t *testing.T) {
	r := New()
	r.Seed([]Rule{
		{Ingress: "in-0", Identity: "vip", Dialer: "fast"},
		{Ingress: "in-0", Dialer: "normal"},
	})

	d, ok := r.Match(rainnet.Meta{Listener: "in-0", Identity: "vip", Target: "example.com:443"})
	if !ok || d.Dialer != "fast" {
		t.Fatalf("vip match = %+v %v", d, ok)
	}
	d, _ = r.Match(rainnet.Meta{Listener: "in-0", Identity: "guest"})
	if d.Dialer != "normal" {
		t.Fatalf("guest match = %+v", d)
	}
}

func TestNoMatch(t *testing.T) {
	r := New()
	r.Seed([]Rule{{Ingress: "only-this", Dialer: "x"}})
	if _, ok := r.Match(rainnet.Meta{Listener: "other"}); ok {
		t.Fatal("expected no match")
	}
}

func TestRoundRobinAndFailover(t *testing.T) {
	r := New()
	rule := Rule{Ingress: "in", Dialer: "d", Streams: []string{"s1", "s2", "s3"}}
	r.Seed([]Rule{rule})

	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		d, ok := r.Match(rainnet.Meta{Listener: "in"})
		if !ok || d.Dialer != "d" {
			t.Fatalf("match %d = %+v %v", i, d, ok)
		}
		seen[d.Stream]++
	}
	for _, s := range []string{"s1", "s2", "s3"} {
		if seen[s] != 2 {
			t.Fatalf("stream %s seen %d times, want 2", s, seen[s])
		}
	}

	// 移除规则后不再命中
	if !r.Remove(rule) {
		t.Fatal("remove should succeed")
	}
	if r.Remove(rule) {
		t.Fatal("second remove should fail")
	}
	if _, ok := r.Match(rainnet.Meta{Listener: "in"}); ok {
		t.Fatal("expected no match after remove")
	}
}

func TestDuplicateAdd(t *testing.T) {
	r := New()
	rule := Rule{Ingress: "in", Target: "db:5432", Dialer: "db-dialer"}
	if !r.Add(rule) {
		t.Fatal("first add should succeed")
	}
	if r.Add(rule) {
		t.Fatal("duplicate add should fail")
	}
	if got := len(r.Rules()); got != 1 {
		t.Fatalf("rules = %d, want 1", got)
	}
}

func TestMatchCarriesRule(t *testing.T) {
	r := New()
	rule := Rule{Ingress: "in", Identity: "client-0", Dialer: "office", Streams: []string{"svc-a"}}
	r.Seed([]Rule{rule})

	d, ok := r.Match(rainnet.Meta{Listener: "in", Identity: "client-0"})
	if !ok || d.Rule.Ingress != "in" || d.Rule.Identity != "client-0" || d.Stream != "svc-a" {
		t.Fatalf("decision = %+v %v", d, ok)
	}
}
