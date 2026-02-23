// cmd/nrf/main.go — NRF process entry point.
//
// Usage:
//
//	go run ./cmd/nrf
package main

import (
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/nrf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim NRF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	cfg := nrf.DefaultConfig()
	n := nrf.New(cfg)

	if err := n.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[NRF] Fatal: %v\n", err)
		os.Exit(1)
	}
}
