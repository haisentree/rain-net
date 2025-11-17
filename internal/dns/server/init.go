package dnsserver

import "rain-net/pluginer"

const serverType = "dns"

func init() {
	pluginer.RegisterServerType(serverType, pluginer.ServerType{
		Directives: func() []string { return Directives },
	},
	)
}

// dns支持的插件
var Directives = []string{
	"root",
	"metadata",
	"geoip",
	"cancel",
	"tls",
	"quic",
	"timeouts",
	"multisocket",
	"reload",
	"nsid",
	"bufsize",
	"bind",
	"debug",
	"trace",
	"ready",
	"health",
	"pprof",
	"prometheus",
	"errors",
	"log",
	"dnstap",
	"local",
	"dns64",
	"acl",
	"any",
	"chaos",
	"loadbalance",
	"tsig",
	"cache",
	"rewrite",
	"header",
	"dnssec",
	"autopath",
	"minimal",
	"template",
	"transfer",
	"hosts",
	"route53",
	"azure",
	"clouddns",
	"k8s_external",
	"kubernetes",
	"file",
	"auto",
	"secondary",
	"etcd",
	"loop",
	"forward",
	"grpc",
	"erratic",
	"whoami",
	"on",
	"sign",
	"view",
	"nomad",
}
