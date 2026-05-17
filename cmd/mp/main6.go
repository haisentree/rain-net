package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	
)

// 视频信息结构
type VideoInfo struct {
	Title    string
	PlayURL  string
	ShareURL string
}

const cookieFile = "douyin_cookies.json"

func main() {
	// 1. 配置浏览器（显示窗口，反检测）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),        // 显示窗口
		chromedp.Flag("hide-scrollbars", false), // 显示滚动条
		chromedp.WindowSize(1920, 1080),         // 窗口大小
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"),
		chromedp.Flag("disable-blink-features", "AutomationControlled"), // 隐藏自动化特征
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// 2. 尝试加载已有 cookies
	if cookies, err := loadCookies(); err == nil && len(cookies) > 0 {
		err := chromedp.Run(ctx,
			network.SetCookies(cookies),
		)
		if err != nil {
			log.Printf("设置 cookies 失败: %v", err)
		} else {
			log.Println("成功加载已有 cookies")
		}
	} else {
		log.Println("未找到 cookies 文件或 cookies 为空，将进行登录操作")
	}

	// 3. 导航到目标用户主页
	targetURL := "https://www.douyin.com/user/MS4wLjABAAAARXMDHun8_uEhSnr0JiV5Zp_bA1gpnyBFAnbtIVGFSE4"
	err := chromedp.Run(ctx,
		chromedp.Navigate(targetURL),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		log.Fatal("导航失败:", err)
	}

	// 4. 检查登录状态（是否存在二维码或登录按钮）
	var needLogin bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
            (function() {
                // 检查是否存在登录二维码或登录按钮
                let qrcode = document.querySelector('.qrcode-img, .login-qrcode, [data-e2e="login-qrcode"]');
                let loginBtn = document.querySelector('[data-e2e="login-button"], .login-button');
                return !!(qrcode || loginBtn);
            })();
        `, &needLogin),
	)
	if err != nil {
		log.Printf("检查登录状态失败: %v，假定需要登录", err)
		needLogin = true
	}

	if needLogin {
		log.Println("检测到未登录，请扫码登录...")
		// 等待用户扫码登录（以用户头像作为登录成功标志）
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(`[data-e2e="user-avatar"]`, chromedp.ByQuery),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
			log.Fatal("登录等待超时，请确保已扫码登录")
		}

		// 登录成功后，保存 cookies
		var cookies []*network.CookieParam
		err = chromedp.Run(ctx,
			// network.GetCookies(&cookies),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				// 使用 GetCookies 获取所有 cookies
				cookie, err := network.GetCookies().Do(ctx)
				fmt.Println(cookie)
				return err
			}))
		if err != nil {
			log.Printf("获取 cookies 失败: %v", err)
		} else if err := saveCookies(cookies); err != nil {
			log.Printf("保存 cookies 失败: %v", err)
		} else {
			log.Println("登录成功，已保存 cookies")
		}
	}

	// ========== 5. 以下为视频数据抓取逻辑 ==========
	var videos []VideoInfo

	// 监听网络请求
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			// 过滤抖音视频数据接口
			if strings.Contains(ev.Response.URL, "/aweme/v1/web/aweme/post/") {
				go func(requestID network.RequestID, url string) {
					ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()

					c := chromedp.FromContext(ctx2)
					if c == nil {
						return
					}

					body, err := network.GetResponseBody(requestID).Do(
						cdp.WithExecutor(ctx2, c.Target))
					if err != nil {
						log.Printf("获取响应体失败 [%s]: %v", url, err)
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

	// 执行主抓取任务
	err = chromedp.Run(ctx,
		network.Enable(),

		// 等待作品标签页出现（确保页面加载完成）
		chromedp.WaitVisible(`[data-e2e="user-work-tab"]`, chromedp.ByQuery),

		// 循环滚动加载更多视频
		chromedp.ActionFunc(func(ctx context.Context) error {
			for i := 0; i < 5; i++ {
				err := chromedp.Evaluate(`
                    (function() {
                        var container = document.querySelector('[data-e2e="user-post-list"]');
                        if (container) {
                            container.scrollTop = container.scrollHeight;
                        } else {
                            window.scrollTo(0, document.body.scrollHeight);
                        }
                    })();
                `, nil).Do(ctx)
				if err != nil {
					return err
				}
				time.Sleep(3 * time.Second)
			}
			return nil
		}),

		// 等待最后一批请求处理完成
		chromedp.Sleep(5*time.Second),
	)

	if err != nil {
		log.Fatal("抓取失败:", err)
	}

	// 输出结果
	fmt.Printf("共捕获 %d 个视频\n", len(videos))
	for i, v := range videos {
		fmt.Printf("%d. 标题: %s\n", i+1, v.Title)
		fmt.Printf("   分享链接: %s\n", v.ShareURL)
		fmt.Printf("   播放地址: %s\n", v.PlayURL)
		fmt.Println()
	}
}

// saveCookies 保存 cookies 到文件
func saveCookies(cookies []*network.CookieParam) error {
	data, err := json.Marshal(cookies)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(cookieFile, data, 0644)
}

// loadCookies 从文件加载 cookies
func loadCookies() ([]*network.CookieParam, error) {
	data, err := ioutil.ReadFile(cookieFile)
	if err != nil {
		return nil, err
	}
	var cookies []*network.CookieParam
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}
	return cookies, nil
}
