// cmd/smf/main.go — SMF process entry point.
//
// Usage:
//
//	go run ./cmd/smf
package main

import (
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/smf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim SMF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	cfg := smf.DefaultConfig()
	s, err := smf.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SMF] Fatal: %v\n", err)
		os.Exit(1)
	}

	if err := s.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[SMF] Fatal: %v\n", err)
		os.Exit(1)
	}
}
