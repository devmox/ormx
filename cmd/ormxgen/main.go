package main

import (
	"fmt"
	"ormx/internal/generate"
	"os"
	"path/filepath"
)

func main() {
	// Test
	fmt.Println(generate.Start())

	if len(os.Args) < 2 {
		fmt.Println("Usage: generate <model/filename.go>")
		os.Exit(1)
	}
	arg := os.Args[1]

	// Check that the path is not empty
	if arg == "" {
		fmt.Println("Error: model path must not be empty.")
		os.Exit(1)
	}

	// Check that the file exists
	absPath, err := filepath.Abs(arg)
	if err != nil {
		fmt.Printf("Error resolving path: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Printf("Error: file '%s' does not exist.\n", absPath)
		os.Exit(1)
	}

	fmt.Printf("Generating model for: %s\n", absPath)
}
