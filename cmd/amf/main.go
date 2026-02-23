package main

// AMF - Access and Mobility Management Function
// Ref: TS 23.501, TS 29.518
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
import (
	"fmt"
	"os"

	"github.com/afroash/5g-sim/internal/amf"
)

func main() {
	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║       5g-sim AMF starting        ║")
	fmt.Println("╚══════════════════════════════════╝")

	cfg := amf.DefaultConfig()
	a := amf.New(cfg)

	if err := a.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[AMF] Fatal: %v\n", err)
		os.Exit(1)
	}
}
