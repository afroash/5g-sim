// cmd/amf/main.go — AMF process entry point.
//
// Starts the AMF with default configuration and blocks listening
// for gNB connections on SCTP port 38412.
//
// Usage:
//
//	go run ./cmd/amf
//	go run ./cmd/amf -config /etc/5g-sim/amf.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/afroash/5g-sim/internal/amf"
	"github.com/afroash/5g-sim/pkg/obs"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim AMF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := amf.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = amf.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AMF] Config: %v\n", err)
			os.Exit(1)
		}
	}

	a := amf.New(cfg)

	hub, err := obs.NewHub("./captures")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AMF] Failed to init observability: %v\n", err)
		os.Exit(1)
	}
	a.Hub = hub

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := a.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[AMF] Fatal: %v\n", err)
			sig <- syscall.SIGTERM
		}
	}()

	<-sig
	fmt.Println("\n[AMF] Shutting down — writing captures...")
	if a.Hub != nil {
		a.Hub.Close()
		fmt.Println("[AMF] Captures written ✓")
	}
}
