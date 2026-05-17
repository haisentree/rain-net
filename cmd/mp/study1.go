package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	cu "github.com/Davincible/chromedp-undetected"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type VideoInfo struct {
	Title    string
	PlayURL  string
	ShareURL string
}

func main() {
	userDataDir := "F:\\Project\\Goland\\demo1\\chrome-user-data"
	// userDataDir := "C:\\Users\\lxx\\AppData\\Local\\Google\\Chrome\\User Data"

	// // 覆盖默认的 ExecAllocator 选项，关闭 headless 模式
	// opts := append(chromedp.DefaultExecAllocatorOptions[:],
	// 	chromedp.Flag("headless", false),        // 显示浏览器窗口
	// 	chromedp.Flag("hide-scrollbars", false), // 显示滚动条
	// 	chromedp.WindowSize(1920, 1080),         // 设置窗口大小
	// 	chromedp.UserDataDir(userDataDir),       // 关键：持久化用户数据
	// 	// chromedp.Flag("profile-directory", "Default"), // 或者 "Profile 1"
	// 	// chromedp.Flag("disable-blink-features", "AutomationControlled"),
	// )

	ctx, cancel, err := cu.New(cu.NewConfig(
		// 指定要使用的用户数据目录，实现持久化登录
		cu.WithUserDataDir(userDataDir),
		// 非 headless 模式，显示窗口
		// 注意：Windows 下如果遇到黑屏问题，可能需要安装 Xvfb，但非 headless 模式通常没问题
		// cu.WithHeadless(), // 注释掉，显示窗口
		cu.WithTimeout(60*time.Second), // 增加超时，给登录留时间
	))
	if err != nil {
		log.Fatal("创建 undetected 浏览器失败:", err)
	}

	// // 创建分配器上下文
	// allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	// defer cancel()

	// // 创建浏览器上下文
	// ctx, cancel := chromedp.NewContext(allocCtx)
	// defer cancel()

	// 设置超时
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 存储请求信息的切片（你可以扩展为结构体来保存更多细节）
	// var requests []string
	var videos []VideoInfo

	// 监听网络事件
	// 注意：必须在调用 Run 之前注册监听器
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		// case *network.EventRequestWillBeSent:

		// 	// 当请求将要发送时触发
		// 	req := ev.Request
		// 	requests = append(requests, fmt.Sprintf("[请求] %s %s", req.Method, req.URL))
		case *network.EventResponseReceived:
			// // 当收到响应时触发
			// resp := ev.Response
			// requests = append(requests, fmt.Sprintf("[响应] %s -> %d %s", resp.URL, resp.Status, resp.StatusText))

			// 过滤抖音视频数据接口
			if strings.Contains(ev.Response.URL, "/aweme/v1/web/aweme/post/") {
				go func(requestID network.RequestID, url string) {
					// task2 := func(requestID network.RequestID, url string) {
					ctx2, cancel := context.WithTimeout(ctx, 50*time.Second)
					defer cancel()

					c := chromedp.FromContext(ctx2)
					if c == nil {
						return
					}

					body, err := network.GetResponseBody(ev.RequestID).Do(
						cdp.WithExecutor(ctx2, c.Target))
					if err != nil {
						log.Printf("获取响应体失败 [%s]: %v", ev.Response.URL, err)
						return
					}

					// 解析 JSON
					var result struct {
						AwemeList []struct {
							Desc  string `json:"desc"`
							Video struct {
								PlayAddr struct {
									URLList []string `json:"url_list"`
								} `json:"play_addr"`
							} `json:"video"`
							ShareURL string `json:"share_url"`
						} `json:"aweme_list"`
					}
					if err := json.Unmarshal(body, &result); err != nil {
						log.Printf("JSON解析失败: %v", err)
						return
					}

					// 提取视频信息
					for _, item := range result.AwemeList {
						video := VideoInfo{
							Title:    item.Desc,
							ShareURL: item.ShareURL,
						}
						if len(item.Video.PlayAddr.URLList) > 0 {
							video.PlayURL = item.Video.PlayAddr.URLList[0]
						}
						videos = append(videos, video)
						fmt.Printf("捕获视频: %s\n", video.Title)
					}
				}(ev.RequestID, ev.Response.URL)
			}

		}
	})

	// 启用网络事件（必须显式调用）
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		log.Fatal("启用网络事件失败:", err)
	}

	// 执行任务
	var title string
	err = chromedp.Run(ctx,
		chromedp.Navigate("https://www.douyin.com/user/MS4wLjABAAAABd96z5H-OLpO6UofviBFqRcxpt-b_vDYE1FRIQ0fqtI"),
		chromedp.Sleep(50*time.Second),
		chromedp.Title(&title),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("页面标题: %s", title)
	// for _, msg := range requests {

	// 	fmt.Println(msg)

	// }
	for _, v := range videos {
		fmt.Println(v.Title)
	}
}
