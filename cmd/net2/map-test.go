package main

import (
	"fmt"
	"strings"
)

func main() {
	userTagList := []uint64{1001, 1002, 1003, 1004}
	commitTagMap := make(map[uint64]bool)

	for _, val := range userTagList {
		commitTagMap[uint64(val)] = true
	}

	strTags := []string{}
	for key, _ := range commitTagMap {
		strTags = append(strTags, fmt.Sprintf("%d", key))
	}
	tagIds := strings.Join(strTags, ",")
	fmt.Println(tagIds)
}
