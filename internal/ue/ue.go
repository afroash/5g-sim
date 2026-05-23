// ue.go — UE runtime state.
package ue

import (
	"net"
	"sync"

	"github.com/afroash/5g-sim/internal/gtp"
)

// UE holds the runtime state of the simulated UE.
type UE struct {
	config      Config
	conn        net.Conn    // SCTP connection to the gNB
	allocatedIP string      // UE IP from PDU Session Establishment Accept
	tunnel      *gtp.Tunnel // GTP-U socket for user plane traffic (Part C)

	uplinkTEID   uint32
	downlinkTEID uint32
	pduSessionID uint8

	mu sync.RWMutex

	onStateChange func(InstanceState)
	onPDUActive   func(ip string, dlTEID uint32)

	virtualTun       *virtualTUN
	userPlaneVirtual bool
	icmpReplyCh      chan struct{}
}

// New creates a new UE instance ready to connect to the gNB.
func New(cfg Config) *UE {
	u := &UE{
		config:       cfg,
		uplinkTEID:   1,
		downlinkTEID: 1,
		pduSessionID: 1,
	}
	if cfg.UplinkTEID != 0 {
		u.uplinkTEID = cfg.UplinkTEID
	}
	return u
}

// AllocatedIP returns the PDU session IP when assigned.
func (u *UE) AllocatedIP() string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.allocatedIP
}

// DownlinkTEID returns the gNB-assigned TEID for downlink GTP-U.
func (u *UE) DownlinkTEID() uint32 {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.downlinkTEID
}

// Close tears down SCTP and GTP-U resources.
func (u *UE) Close() {
	if u.conn != nil {
		_ = u.conn.Close()
		u.conn = nil
	}
	if u.tunnel != nil {
		_ = u.tunnel.Close()
		u.tunnel = nil
	}
	if u.virtualTun != nil {
		u.virtualTun.close()
		u.virtualTun = nil
	}
}

func (u *UE) setState(st InstanceState) {
	if u.onStateChange != nil {
		u.onStateChange(st)
	}
}
