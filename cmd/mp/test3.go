package main

import "fmt"

type Nas struct {
	Name string
	Age  *int
}

func main() {
	age := 12

	newNas := Nas{Name: "111", Age: &age}
	oldNas := newNas

	newNas.Name = "222"
	age = 15
	fmt.Println(oldNas.Name)
	fmt.Println(*oldNas.Age)
}
