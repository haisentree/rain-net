package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/haisentree/rain-net/config"
	dnshandler "github.com/haisentree/rain-net/internal/handler/dns"
	"github.com/haisentree/rain-net/internal/star/starserver"
)

// TestDNSHandlerAndExternalAPI DNS 迁移垂直切片(design.md §七 旗舰示例):
//  1. dns ingress(handler dns)经新 schema 装配,zone 精确匹配应答 A 记录;
//  2. APIProvider 聚合:admin API PUT /api/dns/{name}/zones 动态更新 zone(组件自治);
//  3. 更新后查询即时生效(零重启)。
func TestDNSHandlerAndExternalAPI(t *testing.T) {
	root := &config.Root{
		Services: []config.ServiceSpec{{
			Name: "dns-node",
			Ingress: []config.IngressSpec{
				{
					Name:      "dns-in",
					Transport: "tcp",
					Addr:      "127.0.0.1:0",
					Handler:   "dns",
					Settings: config.IngressExtra{
						DNSZones: map[string]map[string]string{
							"example.org": {"www": "10.0.0.1"},
						},
					},
				},
				{
					Name:      "admin-in",
					Transport: "tcp",
					Addr:      "127.0.0.1:0",
					Handler:   "admin",
					Settings:  config.IngressExtra{AdminPassword: "secret"},
				},
			},
		}},
	}

	servers, err := starserver.BuildServersFromRoot(root)
	if err != nil {
		t.Fatal(err)
	}

	var dnsSrv *dnshandler.Handler
	var adminSrv *starserver.AdminServer
	for _, srv := range servers {
		switch c := srv.(type) {
		case *dnshandler.Handler:
			dnsSrv = c
		case *starserver.AdminServer:
			adminSrv = c
		}
	}
	if dnsSrv == nil || adminSrv == nil {
		t.Fatalf("servers = %d, missing dns(%v)/admin(%v)", len(servers), dnsSrv == nil, adminSrv == nil)
	}

	// 测试自建监听注入(生产装配走 transport 注册表)
	dnsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dnsLn.Close()
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer adminLn.Close()
	dnsSrv.UseListener(dnsLn)
	adminSrv.UseListener(adminLn)
	go func() { _ = dnsSrv.Serve(dnsLn) }()
	go func() { _ = adminSrv.Serve(adminLn) }()

	dnsQueryA := func(name string) ([]string, error) {
		client := &dns.Client{Net: "tcp", Timeout: 3 * time.Second}
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(name), dns.TypeA)
		resp, _, err := client.Exchange(m, dnsLn.Addr().String())
		if err != nil {
			return nil, err
		}
		var ips []string
		for _, a := range resp.Answer {
			if arec, ok := a.(*dns.A); ok {
				ips = append(ips, arec.A.String())
			}
		}
		return ips, nil
	}

	// 1) 静态 zone 应答
	ips, err := dnsQueryA("www.example.org")
	if err != nil || len(ips) != 1 || ips[0] != "10.0.0.1" {
		t.Fatalf("www.example.org = %v, %v; want [10.0.0.1]", ips, err)
	}

	// 2) API 动态更新 zone:改 www + 新增 api
	zones := map[string]map[string]string{
		"example.org": {"www": "10.0.0.2", "api": "10.0.0.3"},
	}
	body, _ := json.Marshal(zones)
	req, _ := http.NewRequest(http.MethodPut,
		"http://"+adminLn.Addr().String()+"/api/dns/dns-in/zones",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT zones = %d", resp.StatusCode)
	}

	// 3) 更新即时生效
	ips, _ = dnsQueryA("www.example.org")
	if len(ips) != 1 || ips[0] != "10.0.0.2" {
		t.Fatalf("www after update = %v, want [10.0.0.2]", ips)
	}
	ips, _ = dnsQueryA("api.example.org")
	if len(ips) != 1 || ips[0] != "10.0.0.3" {
		t.Fatalf("api after update = %v, want [10.0.0.3]", ips)
	}

	// 4) 鉴权:错误 token 被拒
	req, _ = http.NewRequest(http.MethodPut,
		"http://"+adminLn.Addr().String()+"/api/dns/dns-in/zones",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
	}

	_ = fmt.Sprintf
}
