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

	"github.com/afroash/5g-sim/internal/gnb"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim gNB starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	cfg := gnb.DefaultConfig()
	g := gnb.New(cfg)

	if err := g.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[gNB] Fatal: %v\n", err)
		os.Exit(1)
	}
}
