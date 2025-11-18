package custom

import "rain-net/pluginer"

func Run() {
	pluginer.TrapSignalsCrossPlatform()

	inst, err := pluginer.Start()
	if err != nil {
		panic(err)
	}

	inst.Wait()
}
