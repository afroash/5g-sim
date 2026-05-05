// cmd/gnb/main.go — gNB simulator process entry point.
//
// Connects to the AMF and completes NG Setup.
//
// Usage:
//
//	go run ./cmd/gnb
//	go run ./cmd/gnb -config /etc/5g-sim/gnb.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/afroash/5g-sim/internal/gnb"
	"github.com/afroash/5g-sim/pkg/obs"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim gNB starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := gnb.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = gnb.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[gNB] Config: %v\n", err)
			os.Exit(1)
		}
	}

	hub, err := obs.NewHub("./captures-gnb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gNB] Observability init failed: %v\n", err)
		// non-fatal — continue without capture
	}
	cfg.Hub = hub

	g := gnb.New(cfg)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\n[gNB] Shutting down — writing captures...")
		if hub != nil {
			hub.Close()
		}
		os.Exit(0)
	}()

	if err := g.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[gNB] Fatal: %v\n", err)
		os.Exit(1)
	}
}
