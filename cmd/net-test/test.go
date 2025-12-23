package main

import (
	"fmt"
	"time"
)

type Animinl struct {
	head string
	foot string
}

func main() {
	var zeroTime time.Time
	UpdateAt := time.Now()

	zeroTime = zeroTime.Local()

	if UpdateAt.After(zeroTime) {
		fmt.Println("UpdateAt is before zeroTime")
	}
	fmt.Println("zeroTime:", zeroTime)
	fmt.Println("UpdateAt:", UpdateAt)
}
