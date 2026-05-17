package main

import (
	"io"
	"net/http"
	"os"
)

func main() {
	// 从 JSON 中提取一个可用的 URL（建议用前两个 CDN 链接）
	// videoURL := "https://v26-web.douyinvod.com/7a2b78e3caf6e53eddc05bf88b83d8d9/69c56b69/video/tos/cn/tos-cn-ve-0015c800/ooAhanQgCeAFGE9Cdk9d2sfHt9DDABpIagVoFq/?a=6383&ch=10010&cr=3&dr=0&lr=all&cd=0%7C0%7C0%7C3&cv=1&br=2788&bt=2788&cs=0&ds=4&ft=AJkeU_TERR0sTHC4NDv2Nc0iPMgzbLwdpX-U_4hMZSiJNv7TGW&mime_type=video_mp4&qs=0&rc=Njw7ZGZnaWg7aGg6aGQ3ZEBpamg1O3U5cnN3NTMzNGkzM0AyYjItYl42XmIxYzAuX2IwYSNlbnIuMmRzcm1hLS1kLS9zcw%3D%3D&btag=80000e00028000&cquery=101r_100B_100x_100z_100o&dy_q=1774534908&feature_id=0ea98fd3bdc3c6c14a3d0804cc272721&l=20260326222148D45E79260A810B1D53AE"
	videoURL := "https://v26-web.douyinvod.com/84037c5072ee0a212031ef924d61e4a4/69c6bb4f/video/tos/cn/tos-cn-ve-15c000-ce/oAEW3iCeeB0D4xAvwwAjAOCMIfopbLEit5gAlA/media-audio-und-mp4a/?a=6383&br=54&bt=54&btag=c0000e00028000&cd=0%7C0%7C0%7C3&ch=0&cquery=101s_100o&cr=8&cs=4&cv=1&dr=0&dy_q=1774545237&er=1&l=2026032701135705C9FD05D604BFA133F6&lr=default&mime_type=video_mp4&qs=0&rc=NmY6ZjY3ODtkZTlpZTVoOkBpMzs3cnE5cm5lOjMzbGkzNEAyLV8vXi5jXy1fNi8xLWMtYSNfbXJhMmRzYy9hLS1kLWJzcw%3D%3D&temp=1"
	// 创建 HTTP 客户端
	client := &http.Client{}

	// 构造请求
	req, err := http.NewRequest("GET", videoURL, nil)
	if err != nil {
		panic(err)
	}

	// 设置请求头，模拟浏览器
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Referer", "https://www.douyin.com/")
	// 可选：添加 Accept 等
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en-US;q=0.3,en;q=0.2")

	// 发起请求
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		panic("HTTP 状态码异常：" + resp.Status)
	}

	// 创建本地文件保存视频
	out, err := os.Create("video.mp4")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	// 将响应体写入文件
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		panic(err)
	}

	println("下载完成：video.mp4")
}
