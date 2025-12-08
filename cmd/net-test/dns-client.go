package main

import (
	"fmt"
	"log"

	"github.com/miekg/dns"
)

func main() {
	// 1. 创建DNS客户端
	c := new(dns.Client)

	// 2. 构建DNS查询消息
	m := new(dns.Msg)
	m.SetQuestion("baidu.com.", dns.TypeA) // 注意域名末尾的“.”
	m.RecursionDesired = true              // 请求递归查询

	// 3. 向指定服务器发送查询 (例如 Google DNS: 8.8.8.8:53)
	r, _, err := c.Exchange(m, "0.0.0.0:54")
	if err != nil {
		log.Fatal("查询失败:", err)
	}

	// 4. 处理响应
	if r.Rcode != dns.RcodeSuccess {
		log.Fatal("查询不成功:", dns.RcodeToString[r.Rcode])
	}

	// 5. 遍历并打印答案
	for _, ans := range r.Answer {
		// 使用类型断言判断记录类型
		if a, ok := ans.(*dns.A); ok {
			fmt.Printf("%s 的 IPv4 地址是： %s\n", a.Hdr.Name, a.A)
		}
		// 可以类似地处理其他类型，如 *dns.AAAA (IPv6), *dns.CNAME 等
	}
}
