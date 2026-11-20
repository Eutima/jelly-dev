package main

import (
	"fmt"
	"strings"
)

// greetPerson prints a friendly greeting for one person.
func greetPerson(name string, times int) {
	for i := 0; i < times; i++ {
		fmt.Println("Hello there, " + name + "!")
	}
}

// shout prints the name in capital letters.
func shout(name string) {
	fmt.Println(strings.ToUpper(name) + "!!!")
}

// whisper prints the name in lower case.
func whisper(name string) {
	fmt.Println(strings.ToLower(name))
}

func main() {
	greetPerson("world", 2)
	shout("world")
	whisper("WORLD")
}
