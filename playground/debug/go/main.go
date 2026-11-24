// A small program to try the debugger: put a breakpoint in price() and press ctrl+f5.
package main

import "fmt"

type Order struct {
	ID    int
	Items []string
	Total float64
}

func price(item string) float64 {
	prices := map[string]float64{"apple": 1.2, "pear": 0.8, "plum": 2.5}
	return prices[item]
}

func total(order *Order) float64 {
	sum := 0.0
	for _, item := range order.Items {
		sum += price(item)
	}
	return sum
}

func main() {
	order := &Order{ID: 7, Items: []string{"apple", "plum", "pear"}}
	fmt.Println("order", order.ID, "has", len(order.Items), "items")
	order.Total = total(order)
	fmt.Println("order", order.ID, "costs", order.Total)
}
