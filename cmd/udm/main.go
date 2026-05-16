// cmd/udm/main.go — UDM process entry point.
//
// Usage:
//
//	go run ./cmd/udm
//	go run ./cmd/udm -config configs/udm.yaml
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/udm"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim UDM starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := udm.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = udm.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UDM] Config: %v\n", err)
			os.Exit(1)
		}
	}

	reg, err := udm.LoadSubscribersFromFile(cfg.SubscribersPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[UDM] Subscribers: %v\n", err)
		os.Exit(1)
	}

	s := udm.New(cfg, reg)
	if err := s.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[UDM] Fatal: %v\n", err)
		os.Exit(1)
	}
}
