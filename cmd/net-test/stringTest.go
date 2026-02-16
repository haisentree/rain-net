package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 3)

	for i := 0; i < 9; i++ {
		wg.Add(1)
		// 获取信号量（如果已满会阻塞）
		semaphore <- struct{}{}

		go func(rowK int) {
			defer wg.Done()

			defer func() {
				// 释放信号量
				<-semaphore
			}()

			fmt.Printf("开始执行任务：%d\n", i)
			time.Sleep(1 * time.Second)
			fmt.Printf("任务：%d 完成\n", i)
		}(i)
	}

}
