// cmd/ue/main.go — Standalone UE process entry point.
//
// Connects to the gNB over SCTP and drives UE registration and PDU session
// establishment. After the session is up it creates a TUN interface and runs
// a connectivity test.
//
// Usage:
//
//	go run ./cmd/ue
//	go run ./cmd/ue -config /etc/5g-sim/ue.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/afroash/5g-sim/internal/ue"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║        5g-sim UE starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := ue.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = ue.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UE] Config: %v\n", err)
			os.Exit(1)
		}
	}

	u := ue.New(cfg)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\n[UE] Shutting down...")
		os.Exit(0)
	}()

	if err := u.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[UE] Fatal: %v\n", err)
		os.Exit(1)
	}
}
