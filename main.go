// Command services: Go services on the tailnet: lists, workouts
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "services:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fmt.Println(greeting(args))
	return nil
}

func greeting(args []string) string {
	if len(args) > 0 {
		return "hello, " + args[0]
	}
	return "hello"
}
