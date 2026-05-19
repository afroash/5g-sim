// cmd/ue/main.go — UE supervisor or single-instance entry point.
//
// Supervisor (default): HTTP API on :9080, auto-starts UE-001, spawns more on request.
//
//	go run ./cmd/ue
//	go run ./cmd/ue -profile local|clab
//	go run ./cmd/ue -listen 127.0.0.1:9080
//
// Single instance (ContainerLab / debugging):
//
//	go run ./cmd/ue -instance -config /etc/5g-sim/ue.yaml
package main

import (
	"context"
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

	var (
		configPath   string
		profileFlag  string
		listenAddr   string
		instanceMode bool
		noDefault    bool
	)
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.StringVar(&profileFlag, "profile", "", "connection preset: local or clab")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1:9080", "supervisor HTTP listen address")
	flag.BoolVar(&instanceMode, "instance", false, "run a single UE instance (no supervisor)")
	flag.BoolVar(&noDefault, "no-default", false, "supervisor: do not auto-start UE-001")
	flag.Parse()

	base, err := resolveBaseConfig(configPath, profileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[UE] Config: %v\n", err)
		os.Exit(1)
	}

	if instanceMode {
		runInstance(base)
		return
	}
	runSupervisor(base, listenAddr, profileFlag, noDefault)
}

func resolveBaseConfig(configPath, profileFlag string) (ue.Config, error) {
	var cfg ue.Config
	var err error
	if profileFlag != "" {
		cfg, err = ue.BaseConfigForProfile(profileFlag)
		if err != nil {
			return cfg, err
		}
	} else {
		cfg = ue.DefaultConfig()
	}
	if configPath != "" {
		cfg, err = ue.LoadConfigOver(cfg, configPath)
	}
	return cfg, err
}

func runInstance(cfg ue.Config) {
	u := ue.New(cfg)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n[UE] Shutting down...")
		u.Close()
		os.Exit(0)
	}()
	if err := u.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[UE] Fatal: %v\n", err)
		os.Exit(1)
	}
}

func runSupervisor(base ue.Config, listenAddr, profile string, noDefault bool) {
	if profile == "" {
		profile = ue.ProfileLocal
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := ue.NewManager(base, profile)
	if !noDefault {
		if _, err := mgr.SpawnDefault(ctx); err != nil {
			fmt.Printf("[UE supervisor] default instance: %v (will retry via observatory)\n", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n[UE supervisor] Shutting down...")
		cancel()
	}()

	if err := ue.StartSupervisor(ctx, listenAddr, mgr); err != nil {
		fmt.Fprintf(os.Stderr, "[UE supervisor] %v\n", err)
		os.Exit(1)
	}
}
