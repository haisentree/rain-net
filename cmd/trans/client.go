package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	conn, err := net.Dial("tcp", "8.148.84.185:8080")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	data := make([]byte, 64*1024) // 64KB 数据块
	totalBytes := int64(0)
	start := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	// done := make(chan bool)

	go func() {
		for range ticker.C {
			elapsed := time.Since(start).Seconds()
			speed := float64(totalBytes) / elapsed / 1024 // KB/s
			fmt.Printf("\r发送速度: %.2f KB/s, 总计: %d KB", speed, totalBytes/1024)
		}
	}()

	// 发送大量数据，例如 1GB
	for i := 0; i < 1024*1024/64; i++ { // 64KB * 16384 = 1GB
		n, err := conn.Write(data)
		if err != nil {
			break
		}
		totalBytes += int64(n)
	}
	ticker.Stop()
	elapsed := time.Since(start).Seconds()
	fmt.Printf("\n完成，平均速度: %.2f KB/s\n", float64(totalBytes)/elapsed/1024)
}
