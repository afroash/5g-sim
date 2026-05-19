// cmd/observatory/main.go — 5G Observatory sidecar for local NF simulation.
//
// Usage:
//
//	go run ./cmd/observatory
//	go run ./cmd/observatory -config configs/observatory.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/afroash/5g-sim/internal/observatory"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║   5g-sim Observatory starting    ║")
	fmt.Println("╚══════════════════════════════════╝")

	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	cfg := observatory.DefaultConfig()
	if configPath != "" {
		var err error
		cfg, err = observatory.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[observatory] %v\n", err)
			os.Exit(1)
		}
	}
	if cfg.RepoRoot == "" {
		cfg.RepoRoot, _ = os.Getwd()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := observatory.NewHub(cfg.EventBuffer)
	poller := observatory.NewPoller(cfg)
	go poller.Run(ctx, 2*time.Second)

	ues := observatory.NewUEManager(cfg)
	if err := ues.EnsureDefaultUE(ctx); err != nil {
		fmt.Printf("[observatory] default UE: %v\n", err)
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ues.Reap()
			}
		}
	}()

	static, err := observatory.StaticFS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[observatory] static UI: %v (API still available)\n", err)
	}

	srv := observatory.NewServer(cfg, hub, poller, ues, static)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[observatory] %v\n", err)
			sig <- syscall.SIGTERM
		}
	}()

	<-sig
	fmt.Println("\n[observatory] Shutting down...")
	cancel()
	time.Sleep(200 * time.Millisecond)
}
