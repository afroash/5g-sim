// cmd/amf/main.go — AMF process entry point.
//
// Starts the AMF with default configuration and blocks listening
// for gNB connections on SCTP port 38412.
//
// Usage:
//
//	go run ./cmd/amf
//
// Environment can be extended later with flags/config file.
package main

import (
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

	cfg := amf.DefaultConfig()
	a := amf.New(cfg)

	hub, err := obs.NewHub("./captures")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AMF] Failed to init observability: %v\n", err)
		os.Exit(1)
	}
	a.Hub = hub

	// Register signal handler BEFORE starting anything.
	// Buffered channel size 1 — signal package requires this.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Run AMF in background so main can block on the signal channel.
	go func() {
		if err := a.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[AMF] Fatal: %v\n", err)
			// Signal main to exit cleanly so captures are still written.
			sig <- syscall.SIGTERM
		}
	}()

	// Block until Ctrl-C or SIGTERM.
	<-sig
	fmt.Println("\n[AMF] Shutting down — writing captures...")
	if a.Hub != nil {
		a.Hub.Close()
		fmt.Println("[AMF] Captures written ✓")
	}
}
