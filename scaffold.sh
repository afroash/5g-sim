#!/bin/bash
# scaffold.sh
# Run from the root of your github.com/afroash/5g-sim repo
# Creates the full project directory structure and binary stubs

set -e

echo "🚀 Scaffolding 5g-sim project..."
echo ""

dirs=(
  # Binaries
  "cmd/amf"
  "cmd/smf"
  "cmd/gnb"
  "cmd/nrf"

  # NGAP layer - wrapper around free5gc/ngap (TS 38.413)
  "internal/ngap"

  # NAS layer - Non-Access Stratum (TS 24.501)
  "internal/nas"

  # SCTP transport - NGAP runs over SCTP (TS 38.412)
  "internal/sctp"

  # Network Functions - core logic
  "internal/nrf"
  "internal/amf"
  "internal/smf"
  "internal/gnb"

  # 5G Procedures - state machines (TS 23.502)
  "internal/procedures"

  # User Plane - GTP-U tunneling (TS 29.281)
  "internal/gtp"

  # Shared exportable packages
  "pkg/pcap"
  "pkg/logger"

  # Personal spec cross-reference notes
  "specs/notes"
)

for dir in "${dirs[@]}"; do
  mkdir -p "$dir"
  touch "$dir/.gitkeep"
  echo "  ✓ $dir"
done

echo ""
echo "📦 Creating binary entry points..."

cat >cmd/amf/main.go <<'GOEOF'
package main

import "fmt"

// AMF - Access and Mobility Management Function
// Ref: TS 23.501, TS 29.518
func main() {
	fmt.Println("AMF starting...")
}
GOEOF

cat >cmd/smf/main.go <<'GOEOF'
package main

import "fmt"

// SMF - Session Management Function
// Ref: TS 23.501, TS 29.502
func main() {
	fmt.Println("SMF starting...")
}
GOEOF

cat >cmd/gnb/main.go <<'GOEOF'
package main

import "fmt"

// gNB - Next Generation NodeB simulator
// Ref: TS 38.401
func main() {
	fmt.Println("gNB starting...")
}
GOEOF

cat >cmd/nrf/main.go <<'GOEOF'
package main

import "fmt"

// NRF - Network Repository Function (service discovery)
// Ref: TS 29.510
func main() {
	fmt.Println("NRF starting...")
}
GOEOF

echo "  ✓ cmd/amf/main.go"
echo "  ✓ cmd/smf/main.go"
echo "  ✓ cmd/gnb/main.go"
echo "  ✓ cmd/nrf/main.go"

echo ""
echo "📦 Next: pull in free5GC libraries"
echo ""
echo "   go get github.com/free5gc/aper"
echo "   go get github.com/free5gc/ngap"
echo "   go get github.com/ishidawataru/sctp"
echo ""
echo "✅ Scaffold complete."
echo "   See CLAUDE.md for AI context and README.md for the full build checklist."
