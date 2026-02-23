// Package amf implements the Access and Mobility Management Function.
//
// The AMF is the central NF in the 5G control plane. Every gNB connects
// to it over N2 (NGAP/SCTP). Every UE registers through it. It interfaces
// with the SMF (N11), UDM (N8), AUSF (N12), and NRF (N27).
//
// This file defines the AMF's runtime state — what it knows about the
// gNBs and UEs currently connected to it.
//
// Ref: TS 23.501 §6.2.1 — AMF
// Ref: TS 23.502 §4.2   — Registration procedure
package amf

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Config holds the AMF's startup configuration.
type Config struct {
	// Name is the human-readable AMF name advertised in NGSetupResponse.
	Name string

	// PLMN is the PLMN this AMF serves, e.g. "00101" (MCC=001 MNC=01).
	PLMN string

	// RegionID, SetID, Pointer form the GUAMI — the AMF's global identity.
	// Ref: TS 23.003 §2.10
	RegionID uint8
	SetID    uint8
	Pointer  uint8

	// SCTPPort is the port to listen on. Default: 38412.
	SCTPPort int
}

// DefaultConfig returns a sensible config for local development/testing.
func DefaultConfig() Config {
	return Config{
		Name:     "5g-sim-amf",
		PLMN:     "00101",
		RegionID: 1,
		SetID:    1,
		Pointer:  0,
		SCTPPort: 38412,
	}
}

// RAN represents a connected gNB (Radio Access Node).
// Created when an NGSetupRequest is successfully processed.
//
// Ref: TS 38.413 §9.2.6.1 — NG Setup
type RAN struct {
	// Conn is the underlying SCTP connection to this gNB.
	Conn net.Conn

	// GlobalRanNodeID is the gNB's unique identity within the PLMN.
	// Ref: TS 38.413 §9.3.1.5
	GlobalRanNodeID string

	// Name is the optional human-readable gNB name from RANNodeName IE.
	Name string

	// SupportedTACs is the list of Tracking Area Codes this gNB serves.
	// The AMF uses this for paging and mobility decisions.
	SupportedTACs [][]byte

	// ConnectedAt is when this gNB successfully completed NG Setup.
	ConnectedAt time.Time
}

// String returns a readable identifier for this RAN — useful in logs.
func (r *RAN) String() string {
	if r.Name != "" {
		return fmt.Sprintf("%s(%s)", r.Name, r.GlobalRanNodeID)
	}
	return r.GlobalRanNodeID
}

// AMF is the runtime instance of the Access and Mobility Management Function.
// It holds all connected gNBs and registered UE contexts.
type AMF struct {
	config Config

	// mu protects the maps below — multiple gNBs connect concurrently.
	mu sync.RWMutex

	// rans maps the SCTP remote address string → RAN context.
	// Key: conn.RemoteAddr().String()
	rans map[string]*RAN

	// ues holds all UE contexts indexed by NGAP ID and SUPI.
	ues *ueStore
}

// New creates and returns a new AMF instance ready to be started.
func New(cfg Config) *AMF {
	return &AMF{
		config: cfg,
		rans:   make(map[string]*RAN),
		ues:    newUEStore(),
	}
}

// AddRAN stores a newly connected gNB context.
func (a *AMF) AddRAN(conn net.Conn, ran *RAN) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rans[conn.RemoteAddr().String()] = ran
	fmt.Printf("[AMF] gNB registered: %s (total: %d)\n", ran, len(a.rans))
}

// RemoveRAN removes a gNB context when its SCTP connection drops.
func (a *AMF) RemoveRAN(conn net.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := conn.RemoteAddr().String()
	if ran, ok := a.rans[key]; ok {
		fmt.Printf("[AMF] gNB disconnected: %s\n", ran)
		delete(a.rans, key)
	}
}

// GetRAN retrieves the RAN context for a given connection, if it exists.
func (a *AMF) GetRAN(conn net.Conn) (*RAN, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ran, ok := a.rans[conn.RemoteAddr().String()]
	return ran, ok
}

// RANCount returns how many gNBs are currently connected.
func (a *AMF) RANCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.rans)
}

// Config returns the AMF's configuration.
func (a *AMF) Config() Config {
	return a.config
}
