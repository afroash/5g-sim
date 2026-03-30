// cmd/gnb/main.go — gNB simulator process entry point.
//
// Connects to the AMF at 127.0.0.1:38412 and completes NG Setup.
//
// Usage:
//
//	# Terminal 1
//	go run ./cmd/amf
//
//	# Terminal 2
//	go run ./cmd/gnb
package main

import (
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

	hub, err := obs.NewHub("./captures-gnb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gNB] Observability init failed: %v\n", err)
		// non-fatal — continue without capture
	}

	cfg := gnb.DefaultConfig()
	cfg.Hub = hub
	g := gnb.New(cfg)

	// Graceful shutdown
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
