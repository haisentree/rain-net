package httPproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"rain-net/internal/star/plugin"
	"rain-net/protocol/star"
	"time"
)

type HttpProxy struct {
	Next plugin.Handler
}

func (p HttpProxy) ServeStar(ctx context.Context, w star.ResponseWriter, data []byte) error {
	w.SetKeepAlive(true)

	reader := w.GetReader()
	req, err := http.ReadRequest(reader)
	if err != nil {
		w.SetKeepAlive(false)
		return err
	}
	defer req.Body.Close()

	fmt.Println(req.Method)
	if req.Method == http.MethodConnect {
		err := handleCONNECT(w, req)
		if err != nil {
			w.SetKeepAlive(false)
			return err
		}
	} else {
		handleHTTP(w, req)
	}

	// if p.Next != nil {
	// 	p.Next.ServeStar(ctx, w, data)
	// }
	return nil
}

func (p HttpProxy) Name() string { return "HttpProxy" }

// 处理 HTTP 请求（非 CONNECT）
func handleHTTP(w star.ResponseWriter, r *http.Request) {
	// 构建目标 URL
	targetURL := r.URL
	// 如果 URL 不完整（例如客户端只发了路径），需要补全 Scheme 和 Host
	if targetURL.Scheme == "" {
		targetURL.Scheme = "http"
	}
	if targetURL.Host == "" {
		targetURL.Host = r.Host
	}

	// 创建一个新请求，复制原请求的 Header 和 Body
	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
	if err != nil {
		w.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	proxyReq.Header = r.Header.Clone()

	// 使用 http.Client 发送请求（可以设置超时、禁用重定向等）
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if err := resp.Write(w); err != nil {
		fmt.Println("resp.Write")
		return
	}
}

func handleCONNECT(w star.ResponseWriter, req *http.Request) error {
	fmt.Println("req.Host:", req.Host)

	targetConn, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		return err
	}
	defer targetConn.Close()

	b := []byte("HTTP/1.1 200 Connection established" + "\r\n\r\n")
	_, err = w.Write(b)
	if err != nil {
		return err
	}
	clientReader := w.GetReader()
	// star.ResponseWriter 应该同时实现了 io.Writer（通过 Write 方法）和可能 io.Reader？
	// 这里我们直接使用 clientReader 和 w 进行双向复制
	// 注意：w 是写入端，clientReader 是读取端

	// 双向数据复制
	errChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(targetConn, clientReader)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(w, targetConn)
		errChan <- err
	}()

	// 等待任意一个复制结束或上下文取消
	select {
	case <-errChan:
		// 其中一个方向结束，关闭连接
		return errors.New("errChan")
	}

	return nil
}
