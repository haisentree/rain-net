package main

import "rain-net/pluginer"

func main() {
	pluginer.TrapSignalsCrossPlatform()

	inst, err := pluginer.Start()
	if err != nil {
		panic(err)
	}

	inst.Wait()
}
