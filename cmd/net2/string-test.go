package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	argsStruct := "123"
	requestBodyJson, _ := json.Marshal(argsStruct)
	fmt.Println(string(requestBodyJson))
}
