package main

import (
	"fmt"
	"time"
	"math"
)

func main() {
	fmt.Println("Hello, 世界")
	fmt.Println("The time is", time.Now())
	// fmt.Println("My favorite number is", rand.Intn(10))
	fmt.Println("the pi constant is", math.Pi)

	fmt.Println("the result is",addthem(4,5))

	sum := 0
	for i := 0; i < 10; i++ {
		sum += i
		fmt.Println("adding ",i)
	}
	fmt.Println(sum)
}

func addthem(a int, b int) int {
	return a + b
}
