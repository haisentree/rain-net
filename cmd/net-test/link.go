package main

import "fmt"

// 处理者接口
type Handler interface {
	SetNext(handler Handler) Handler
	HandleRequest(request string)
}

// 基础处理者结构体
type BaseHandler struct {
	next Handler
}

// 设置下一个处理者
func (b *BaseHandler) SetNext(handler Handler) Handler {
	b.next = handler
	return handler
}

// 调用下一个处理者
func (b *BaseHandler) HandleRequest(request string) {
	if b.next != nil {
		b.next.HandleRequest(request)
	}
}

// 具体处理者1：经理审批
type ManagerHandler struct {
	BaseHandler
}

func (m *ManagerHandler) HandleRequest(request string) {
	if request == "小额报销" {
		fmt.Println("经理处理了请求:", request)
	} else {
		fmt.Println("经理将请求传递给下一位处理者")
		m.BaseHandler.HandleRequest(request)
	}
}

// 具体处理者2：总监审批
type DirectorHandler struct {
	BaseHandler
}

func (d *DirectorHandler) HandleRequest(request string) {
	if request == "大额报销" {
		fmt.Println("总监处理了请求:", request)
	} else {
		fmt.Println("总监将请求传递给下一位处理者")
		d.BaseHandler.HandleRequest(request)
	}
}

// 具体处理者3：CEO审批
type CEOHandler struct {
	BaseHandler
}

func (c *CEOHandler) HandleRequest(request string) {
	fmt.Println("CEO最终处理了请求:", request)
}

// 主函数
func main() {
	// 创建处理者
	manager := &ManagerHandler{}
	director := &DirectorHandler{}
	ceo := &CEOHandler{}

	// 构建责任链
	manager.SetNext(director).SetNext(ceo)

	// 测试责任链
	fmt.Println("发送小额报销请求:")
	manager.HandleRequest("小额报销")

	fmt.Println("\n发送大额报销请求:")
	manager.HandleRequest("大额报销")

	fmt.Println("\n发送特殊请求:")
	manager.HandleRequest("特殊请求")
}
