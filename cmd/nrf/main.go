// cmd/nrf/main.go — NRF process entry point.
//
// Usage:
//
//	go run ./cmd/nrf
//	go run ./cmd/nrf -config /etc/5g-sim/nrf.yaml
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/nrf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim NRF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := nrf.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = nrf.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[NRF] Config: %v\n", err)
			os.Exit(1)
		}
	}

	n := nrf.New(cfg)
	if err := n.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[NRF] Fatal: %v\n", err)
		os.Exit(1)
	}
}
