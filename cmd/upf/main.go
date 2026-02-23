// cmd/upf/main.go — UPF process entry point.
//
// The UPF listens on GTP-U port 2152 for encapsulated UE IP packets.
// Sessions are registered when the SMF calls RegisterSession (PFCP simulation).
//
// Usage:
//
//	go run ./cmd/upf
package main

import (
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/upf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim UPF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	cfg := upf.DefaultConfig()
	u, err := upf.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[UPF] Fatal: %v\n", err)
		os.Exit(1)
	}

	// Start PFCP-sim API so the SMF can register sessions.
	if err := u.StartPFCPSim(8002); err != nil {
		fmt.Fprintf(os.Stderr, "[UPF] PFCP-sim start failed: %v\n", err)
		os.Exit(1)
	}

	// Start GTP-U tunnel — blocks until shutdown.
	u.Start()
}
