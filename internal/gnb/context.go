// Package gnb implements the gNB (Next Generation NodeB) simulator.
//
// The gNB is the 5G base station — it sits between UEs (phones) and the
// 5G core network. In our simulator it plays the radio access side,
// connecting to the AMF over N2 (NGAP/SCTP) and driving procedures.
//
// The gNB always initiates the SCTP association to the AMF.
// Ref: TS 38.401 — NG-RAN Architecture
// Ref: TS 38.412 §5.1 — gNB initiates the SCTP association
package gnb

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/afroash/5g-sim/pkg/obs"
)

// Config holds the gNB's startup configuration.
type Config struct {
	// GlobalGNBID is the 28-bit gNB identity within the PLMN.
	// Ref: TS 38.413 §9.3.1.6
	GlobalGNBID uint32

	// Name is the human-readable gNB name (RANNodeName IE).
	Name string

	// PLMN is the PLMN this gNB belongs to, e.g. "00101".
	PLMN string

	// TAC is the Tracking Area Code this gNB covers (3 bytes).
	// Ref: TS 23.003 §19.4
	TAC uint32

	// AMFAddress is the IP/hostname of the AMF to connect to.
	AMFAddress string

	// Hub is the optional observability hub for packet capture.
	Hub *obs.Hub

	// AMFPort is the SCTP port of the AMF. Default: 38412.
	AMFPort int
}

// DefaultConfig returns a sensible gNB config for local testing.
// Matches the test PLMN used in the AMF's DefaultConfig.
func DefaultConfig() Config {
	return Config{
		GlobalGNBID: 0x1234,
		Name:        "5g-sim-gnb-01",
		PLMN:        "00101",
		TAC:         0x000001,
		AMFAddress:  "127.0.0.1",
		AMFPort:     38412,
	}
}

// AMFContext holds what the gNB learns about the AMF after NG Setup.
// The gNB needs this to route UE messages correctly.
//
// Ref: TS 38.413 §9.2.6.2 — NGSetupResponse IEs
type AMFContext struct {
	// Name is the AMF's human-readable name from NGSetupResponse.
	Name string

	// GUAMI is the Globally Unique AMF Identifier.
	// gNB uses this to identify which AMF a UE is registered with.
	// Ref: TS 23.003 §2.10
	GUAMIRegion  uint8
	GUAMISet     uint8
	GUAMIPointer uint8

	// PLMNs is the list of PLMNs and slices the AMF supports.
	PLMNs []string

	// Capacity is the AMF's relative load capacity (0-255).
	// Used by the gNB for AMF selection when multiple AMFs exist.
	Capacity int64

	// SetupAt is when NG Setup completed successfully.
	SetupAt time.Time
}

// GNB is the runtime instance of the gNB simulator.
type GNB struct {
	config Config

	mu sync.RWMutex

	// conn is the SCTP connection to the AMF. Nil until connected.
	conn net.Conn

	// amf holds what we know about the AMF after NG Setup.
	// Nil until NGSetupResponse is received.
	amf *AMFContext

	// setupDone is closed when NG Setup completes successfully.
	// Lets other goroutines wait for the gNB to be ready.
	setupDone chan struct{}

	// pendingULTEID is the UL TEID the UPF expects for uplink GTP-U packets.
	// Populated from the SMF session response, passed via the AMF.
	pendingULTEID uint32

	// pendingUPFAddr is the UPF GTP-U endpoint ("ip:port").
	// Populated from the SMF session response, passed via the AMF.
	pendingUPFAddr string

	// Hub is the observability hub (nil if not configured).
	Hub *obs.Hub
}

// New creates a new gNB instance ready to connect to the AMF.
func New(cfg Config) *GNB {
	return &GNB{
		config:    cfg,
		setupDone: make(chan struct{}),
		Hub:       cfg.Hub,
	}
}

// SetConn stores the SCTP connection to the AMF.
func (g *GNB) SetConn(conn net.Conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.conn = conn
}

// SetAMFContext stores the AMF context learned from NGSetupResponse
// and signals that setup is complete.
func (g *GNB) SetAMFContext(amf *AMFContext) {
	g.mu.Lock()
	g.amf = amf
	g.mu.Unlock()

	// Signal setup complete — non-blocking in case it's already closed.
	select {
	case <-g.setupDone:
	default:
		close(g.setupDone)
	}

	fmt.Printf("[gNB] NG Setup complete — AMF: %s  GUAMI: %d/%d/%d  Capacity: %d\n",
		amf.Name, amf.GUAMIRegion, amf.GUAMISet, amf.GUAMIPointer, amf.Capacity)
}

// WaitForSetup blocks until NG Setup completes or the timeout fires.
// Returns true if setup completed, false if timed out.
func (g *GNB) WaitForSetup(timeout time.Duration) bool {
	select {
	case <-g.setupDone:
		return true
	case <-time.After(timeout):
		return false
	}
}

// IsSetup returns true if NG Setup has completed successfully.
func (g *GNB) IsSetup() bool {
	select {
	case <-g.setupDone:
		return true
	default:
		return false
	}
}

// AMF returns the AMF context, or nil if NG Setup hasn't completed yet.
func (g *GNB) AMF() *AMFContext {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.amf
}

// Send writes raw bytes to the AMF over the SCTP connection.
func (g *GNB) Send(data []byte) error {
	g.mu.RLock()
	conn := g.conn
	g.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected to AMF")
	}
	_, err := conn.Write(data)
	return err
}

// Config returns the gNB's configuration.
func (g *GNB) Config() Config {
	return g.config
}
