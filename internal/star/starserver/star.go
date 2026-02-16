package starserver

import "rain-net/pluginer"

func Run() {
	// pluginer.TrapSignalsCrossPlatform()

	yamlFileInput := pluginer.YAMLFileInput{
		Filepath:       "/root/Project/DnsGit/rain-net/etc/star.example.yaml",
		Contents:       []byte("star"),
		ServerTypeName: "star",
	}

	inst, err := pluginer.Start(yamlFileInput)
	if err != nil {
		panic(err)
	}

	inst.Wait()
}
