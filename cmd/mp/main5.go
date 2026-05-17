package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func main() {
	// 1. 配置浏览器（显示窗口，反检测）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),                                                   // 显示窗口
		chromedp.Flag("hide-scrollbars", false),                                            // 显示滚动条
		chromedp.WindowSize(1920, 1080),                                                    // 窗口大小
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"), // 真实 UA
		chromedp.Flag("disable-blink-features", "AutomationControlled"),                    // 隐藏自动化特征
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// 设置总超时（可根据需要调整）
	ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// 2. 存储捕获到的响应数据
	var responses []string

	// 3. 监听网络事件
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			// 过滤出您关心的请求（例如抖音的视频数据接口）
			url := ev.Response.URL
			if strings.Contains(url, "/aweme/v1/web/aweme/post/") ||
				strings.Contains(url, "/aweme/v1/web/feed/") {
				// 注意：这里需要获取响应体，必须在 goroutine 中完成，以避免阻塞事件循环
				go func(requestID network.RequestID, url string) {
					// 为每个请求创建独立上下文
					ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()

					c := chromedp.FromContext(ctx2)
					if c == nil {
						return
					}

					// 获取响应体
					body, err := network.GetResponseBody(requestID).Do(
						cdp.WithExecutor(ctx2, c.Target))
					if err != nil {
						log.Printf("获取响应体失败 [%s]: %v", url, err)
						return
					}

					// 尝试解析 JSON（抖音返回 JSON）
					var result map[string]interface{}
					if err := json.Unmarshal(body, &result); err == nil {
						fmt.Printf("捕获到数据接口: %s\n", url)
						// 这里可以进一步提取您需要的信息，例如视频列表
						// 示例：将原始 JSON 字符串保存下来
						responses = append(responses, string(body))
					}
				}(ev.RequestID, ev.Response.URL)
			}
		}
	})

	// 4. 执行主任务
	err := chromedp.Run(ctx,
		// 启用网络监听
		network.Enable(),

		// 访问目标页面（替换为您需要爬取的抖音用户主页）
		chromedp.Navigate("https://www.douyin.com/user/MS4wLjABAAAABd96z5H-OLpO6UofviBFqRcxpt-b_vDYE1FRIQ0fqtI"),

		// 等待页面初始加载
		chromedp.Sleep(5*time.Second),

		// 循环滚动加载更多内容（模拟下滑）
		chromedp.ActionFunc(func(ctx context.Context) error {
			// 滚动次数可根据需要调整
			for i := 0; i < 5; i++ {
				// 方法1：滚动整个页面（简单但不一定适用于所有无限滚动）
				// chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil).Do(ctx)

				// 方法2：针对抖音的滚动容器（更可靠）
				err := chromedp.Evaluate(`
                    let container = document.querySelector('[data-e2e="user-post-list"]');
                    if (container) {
                        container.scrollTop = container.scrollHeight;
                    } else {
                        window.scrollTo(0, document.body.scrollHeight);
                    }
                `, nil).Do(ctx)
				if err != nil {
					return err
				}

				// 等待新内容加载（适当调整时间）
				time.Sleep(3 * time.Second)
			}
			return nil
		}),

		// 等待足够时间让滚动触发的网络请求被捕获
		chromedp.Sleep(10*time.Second),
	)

	if err != nil {
		log.Fatal("执行失败:", err)
	}

	// 5. 输出捕获到的响应数据
	fmt.Printf("共捕获到 %d 个响应\n", len(responses))
	for i, resp := range responses {
		fmt.Printf("响应 %d: %s\n", i+1, resp[:min(len(resp), 200)]) // 打印前200字符
	}

	// log.Printf("页面标题: %s", title) // 注意：您原代码中 title 变量未定义，需补上
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
