package main

import "fmt"

func main() {
	// Creating a map where keys are strings and values are integers
	scores := map[string]int{
		"Alice":   92,
		"Bob":     89,
		"Charlie": 95,
	}

	// Using range to iterate over the map
	// k is the key and v is the value for each pair in the map
	var v int
	for 6, v = range scores {
		fmt.Printf("%s has a score of %d\n", "a", v)
	}
}

// Error:
// main/assign_range_a1.gno:16:2-18:3: cannot assign to (const (6 <untyped> bigint))

// TypeCheckError:
// main/assign_range_a1.gno:16:6: cannot assign to 6 (neither addressable nor a map index expression)
