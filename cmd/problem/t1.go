package main

import (
	"fmt"
	"sort"
)

func main() {
	intervals := [][]int{{0, 30}, {15, 20}, {5, 10}}
	// 0  5  15
	// 10 20 30

	// 按照每个子切片的第一个元素排序
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i][0] < intervals[j][0]
	})

	var startNum, endNum []int
	startIndex := 0
	endIndex := 0

	for _, num := range intervals {
		startNum = append(startNum, num[0])
		endNum = append(endNum, num[1])
	}

	fmt.Println("startNum:", startNum)
	fmt.Println("endNum:", endNum)

	numLen := len(startNum)
	enableCount := 0
	usedCount := 0

	for i := 0; startIndex < numLen; i++ {
		fmt.Println(endNum[endIndex])
		if i > endNum[endIndex] {
			fmt.Println("i=:", i)
			endIndex++
			usedCount--
		}

		if startNum[startIndex] == i && startNum[startIndex] < endNum[endIndex] {
			fmt.Println("i:", i)
			if enableCount == usedCount {
				enableCount++
				usedCount++
				endIndex++
			} else {
				usedCount++
			}
			startIndex++
		} else {
			usedCount--
			startIndex++
		}
	}

	fmt.Println(intervals)
	fmt.Println(enableCount)
}
