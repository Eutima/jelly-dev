package main

import (
	"fmt"
	"strings"
)

// greet prints a friendly greeting for one person.
func greet(name string) {
	fmt.Println("Hello, " + name + "!")
}

// shout prints the name in capital letters.
func shout(name string) {
	fmt.Println(strings.ToUpper(name))
}

func main() {
	greet("world")
	shout("world")
}
