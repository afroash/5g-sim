// cmd/smf/main.go — SMF process entry point.
//
// Usage:
//
//	go run ./cmd/smf
//	go run ./cmd/smf -config /etc/5g-sim/smf.yaml
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/smf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim SMF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := smf.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = smf.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SMF] Config: %v\n", err)
			os.Exit(1)
		}
	}

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
