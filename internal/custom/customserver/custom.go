package customserver

import "rain-net/pluginer"

func Run() {
	// pluginer.TrapSignalsCrossPlatform()

	yamlFileInput := pluginer.YAMLFileInput{
		Filepath:       "/root/Project/DnsGit/rain-net/etc/custom.yaml",
		Contents:       []byte("custom"),
		ServerTypeName: "custom",
	}

	inst, err := pluginer.Start(yamlFileInput)
	if err != nil {
		panic(err)
	}

	inst.Wait()
}
