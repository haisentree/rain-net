package main

import (
	"fmt"

	"rain-net/pluginer"
)

func main() {
	instance, err := pluginer.Start()
	if err != nil {
		fmt.Println(err)
	}

	instance.Wait()
}
