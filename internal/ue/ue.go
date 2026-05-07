// ue.go — UE runtime state.
package ue

import (
	"net"

	"github.com/afroash/5g-sim/internal/gtp"
)

// UE holds the runtime state of the simulated UE.
type UE struct {
	config      Config
	conn        net.Conn    // SCTP connection to the gNB
	allocatedIP string      // UE IP from PDU Session Establishment Accept
	tunnel      *gtp.Tunnel // GTP-U socket for user plane traffic (Part C)
}

// New creates a new UE instance ready to connect to the gNB.
func New(cfg Config) *UE {
	return &UE{config: cfg}
}
