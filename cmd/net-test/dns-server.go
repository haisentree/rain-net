package main

import (
	"log"
	"net"

	"github.com/miekg/dns"
)

// 定义处理所有DNS请求的函数
func handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	// 1. 创建一个应答消息，并设置为对请求的回复
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true // 声明为权威应答

	// 2. 如果请求中有问题，就构造一个答案
	if len(r.Question) > 0 {
		question := r.Question[0]
		// 示例：为所有A记录查询返回一个固定的IP
		if question.Qtype == dns.TypeA {
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   question.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60, // 缓存时间60秒
				},
				A: net.ParseIP("192.0.2.1").To4(), // 示例IP
			}
			msg.Answer = append(msg.Answer, rr)
		}
	}
	// 3. 将应答写回客户端
	w.WriteMsg(msg)
}

func main() {
	// 将处理函数绑定到所有域名（“.”）
	dns.HandleFunc(".", handleRequest)

	// 启动UDP服务器，监听53端口
	server := &dns.Server{Addr: ":54", Net: "udp"}
	log.Println("DNS服务器启动，监听端口 54...")
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("启动服务器失败: ", err)
	}
}
