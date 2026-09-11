package main

import (
	"fmt"

	"aaa2ppp/cdnnow-test-golang-14/internal/calc"
)

func main() {
	var a, b int64 = 1, 2
	fmt.Printf("%d + %d = %d\n", a, b, calc.Add(a, b))
	fmt.Printf("%d - %d = %d\n", a, b, calc.Sub(a, b))
}
