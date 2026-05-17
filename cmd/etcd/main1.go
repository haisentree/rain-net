package main

import (
	"context"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	// 创建 etcd 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"8.148.84.185:2379"}, // etcd 节点地址
		DialTimeout: 5 * time.Second,               // 连接超时时间
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	ctx := context.Background()

	fmt.Print("=====")
	// 1. 写入键值对
	_, err = cli.Put(ctx, "key1", "value1")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Put key1 success")

	// 2. 读取键值对
	resp, err := cli.Get(ctx, "key1")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("12334")
	for _, ev := range resp.Kvs {
		fmt.Printf("Key: %s, Value: %s\n", ev.Key, ev.Value)
	}

	// 3. 删除键值对
	_, err = cli.Delete(ctx, "key1")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Delete key1 success")
}
