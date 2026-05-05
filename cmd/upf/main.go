// cmd/upf/main.go — UPF process entry point.
//
// The UPF listens on GTP-U port 2152 for encapsulated UE IP packets.
// Sessions are registered when the SMF calls RegisterSession (PFCP simulation).
//
// Usage:
//
//	go run ./cmd/upf
//	go run ./cmd/upf -config /etc/5g-sim/upf.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/afroash/5g-sim/internal/upf"
	"github.com/afroash/5g-sim/pkg/obs"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim UPF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := upf.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = upf.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UPF] Config: %v\n", err)
			os.Exit(1)
		}
	}

	hub, err := obs.NewHub("./captures")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[UPF] Observability init failed: %v\n", err)
		// non-fatal — continue without capture
	}
	cfg.Hub = hub

	u, err := upf.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[UPF] Fatal: %v\n", err)
		os.Exit(1)
	}

	if err := u.StartPFCPSim(cfg.PFCPSimPort); err != nil {
		fmt.Fprintf(os.Stderr, "[UPF] PFCP-sim start failed: %v\n", err)
		os.Exit(1)
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\n[UPF] Shutting down — writing captures...")
		if hub != nil {
			hub.Close()
		}
		os.Exit(0)
	}()

	u.Start()
}
