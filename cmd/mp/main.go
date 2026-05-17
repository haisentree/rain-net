package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func downloadFile(url, filepath string) error {
	// 1. 创建HTTP客户端（可以设置超时）
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	// 2. 发起GET请求
	// 注意：直接下载视频文件URL，而非视频页面URL

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 3. 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，状态码: %s", resp.Status)
	}

	// 4. 创建本地文件
	outFile, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// 5. 使用缓冲区复制数据，避免内存暴涨
	// 创建一个32KB的缓冲区
	buf := make([]byte, 32*1024)
	_, err = io.CopyBuffer(outFile, resp.Body, buf)
	if err != nil && err != io.EOF {
		return err
	}

	fmt.Println("文件下载完成:", filepath)
	return nil
}

func main() {
	// 这是你提供的抖音直接视频链接
	// videoURL := "https://v26-cold.douyinvod.com/4427ec2a3c7feb0e2ef21caf3d4345a9/69b02866/video/tos/cn/tos-cn-ve-15c000-ce/oIFZsC1ppAtrEiyQEh6pu9lCeGmfEhe6A7BwID/?a=1128&ch=0&cr=0&dr=0&cd=0%7C0%7C0%7C0&cv=1&br=1481&bt=1481&cs=0&ds=3&ft=NIyzNp9VtXXsb2jKyO.i~OkriQ3nOz7o8l7opMyteTNiDScXB22eAFFS_kSO_bd.o~&mime_type=video_mp4&qs=0&rc=M2YzZDVmNjpmOzk3ZmRlZkBpajV0eWo5cnY8OTMzbGkzNEBiYi4vYC5fXy4xMmBhNTUvYSMta19yMmRrYGdhLS1kLWJzcw%3D%3D&btag=80010e00090000&cquery=100y&dy_q=1773148742&feature_id=fea919893f650a8c49286568590446ef&l=20260310211902B3B23322A495FF0FB393" // 请替换为完整链接
	//"https://v26-web.douyinvod.com/a44371803b1c0a68cb6a12b6821fd8b3/69c6955a/video/tos/cn/tos-cn-ve-15/oMRAACIgEeGA75Ac7BAkLHTBeMEAFRAB7eRlTb/media-video-hvc1/?a=6383&ch=0&cr=8&dr=0&er=1&lr=default&cd=0%7C0%7C0%7C3&cv=1&br=514&bt=514&cs=4&ds=4&mime_type=video_mp4&qs=0&rc=NDs6ZzQ8MzloNTU5OjQ5OEBpM2h3OG05cnZqNTMzNGkzM0BeYzUuLzNeNjMxY2AzNi8tYSMtMTQtMmRrYmVhLS1kLS9zcw%3D%3D&btag=80000e00038000&cquery=100w_100o&dy_q=1774534742&l=2026032622190212E4DBF5BB78EE182469"
	// videoURL := "https://v26-web.douyinvod.com/7a2b78e3caf6e53eddc05bf88b83d8d9/69c56b69/video/tos/cn/tos-cn-ve-0015c800/ooAhanQgCeAFGE9Cdk9d2sfHt9DDABpIagVoFq/?a=6383\u0026ch=10010\u0026cr=3\u0026dr=0\u0026lr=all\u0026cd=0%7C0%7C0%7C3\u0026cv=1\u0026br=2788\u0026bt=2788\u0026cs=0\u0026ds=4\u0026ft=AJkeU_TERR0sTHC4NDv2Nc0iPMgzbLwdpX-U_4hMZSiJNv7TGW\u0026mime_type=video_mp4\u0026qs=0\u0026rc=Njw7ZGZnaWg7aGg6aGQ3ZEBpamg1O3U5cnN3NTMzNGkzM0AyYjItYl42XmIxYzAuX2IwYSNlbnIuMmRzcm1hLS1kLS9zcw%3D%3D\u0026btag=80000e00028000\u0026cquery=101r_100B_100x_100z_100o\u0026dy_q=1774534908\u0026feature_id=0ea98fd3bdc3c6c14a3d0804cc272721\u0026l=20260326222148D45E79260A810B1D53AE"
	videoURL := "https://v26-web.douyinvod.com/7a2b78e3caf6e53eddc05bf88b83d8d9/69c56b69/video/tos/cn/tos-cn-ve-0015c800/ooAhanQgCeAFGE9Cdk9d2sfHt9DDABpIagVoFq/?a=6383&ch=10010&cr=3&dr=0&lr=all&cd=0%7C0%7C0%7C3&cv=1&br=2788&bt=2788&cs=0&ds=4&ft=AJkeU_TERR0sTHC4NDv2Nc0iPMgzbLwdpX-U_4hMZSiJNv7TGW&mime_type=video_mp4&qs=0&rc=Njw7ZGZnaWg7aGg6aGQ3ZEBpamg1O3U5cnN3NTMzNGkzM0AyYjItYl42XmIxYzAuX2IwYSNlbnIuMmRzcm1hLS1kLS9zcw%3D%3D&btag=80000e00028000&cquery=101r_100B_100x_100z_100o&dy_q=1774534908&feature_id=0ea98fd3bdc3c6c14a3d0804cc272721&l=20260326222148D45E79260A810B1D53AE"
	err := downloadFile(videoURL, "douyin_video.mp4")
	if err != nil {
		fmt.Println("出错了:", err)
	}
}
