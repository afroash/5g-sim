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

	"github.com/afroash/5g-sim/internal/gtp"
	"github.com/afroash/5g-sim/pkg/obs"
)

// Config holds the gNB's startup configuration.
type Config struct {
	// GlobalGNBID is the 28-bit gNB identity within the PLMN.
	// Ref: TS 38.413 §9.3.1.6
	GlobalGNBID uint32 `yaml:"global_gnb_id"`

	// Name is the human-readable gNB name (RANNodeName IE).
	Name string `yaml:"name"`

	// PLMN is the PLMN this gNB belongs to, e.g. "00101".
	PLMN string `yaml:"plmn"`

	// TAC is the Tracking Area Code this gNB covers (3 bytes).
	// Ref: TS 23.003 §19.4
	TAC uint32 `yaml:"tac"`

	// AMFAddress is the IP/hostname of the AMF to connect to.
	AMFAddress string `yaml:"amf_address"`

	// AMFPort is the SCTP port of the AMF. Default: 38412.
	AMFPort int `yaml:"amf_port"`

	// GTPAddress is the IP the gNB uses for its N3 GTP-U endpoint.
	GTPAddress string `yaml:"gtp_address"`

	// UEPort is the SCTP port for accepting UE connections. Default: 38413.
	UEPort int `yaml:"ue_port"`

	// UEGTPPort is the UDP port for the UE-facing GTP-U tunnel. Default: 2153.
	// gNB binds this socket to receive uplink GTP-U from the UE.
	// Ref: TS 29.281 §4.4.2
	UEGTPPort int `yaml:"ue_gtp_port"`

	// UPFGTPPort is the UDP port the UPF listens on for GTP-U. Default: 2152.
	// Used as the destination port when re-encapsulating UE uplink toward the UPF.
	// Ref: TS 29.281 §4.4.2
	UPFGTPPort int `yaml:"upf_gtp_port"`

	// Hub is the optional observability hub for packet capture.
	Hub *obs.Hub `yaml:"-"`
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
		GTPAddress:  "127.0.0.1",
		UEPort:      38413,
		UEGTPPort:   2153,
		UPFGTPPort:  2152,
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

	// pendingDLTEID is the DL TEID allocated in HandlePDUSessionResourceSetupRequest
	// and used when the GTP tunnel is set up.
	pendingDLTEID uint32

	// nextDLTEID is the DL TEID allocation counter.
	nextDLTEID uint32

	// uesByRanID tracks UE relay contexts by RAN-UE-NGAP-ID. Used in Part B.
	uesByRanID map[int64]*UERelayContext

	// userPlane holds the active GTP-U state after PDU session establishment.
	userPlane *UserPlane

	// ueRelay is the gNB's UE-facing GTP-U listener (default port 2153).
	// Started once at boot; demuxes UE uplink across all PDU sessions by
	// inner src IP and relays to the appropriate UPF endpoint.
	// Ref: TS 29.281 §5.1 — G-PDU forwarding
	ueRelay *gtp.Tunnel

	// ueSessions maps RAN-UE-NGAP-ID to its tunnel state. Populated when
	// HandlePDUSessionResourceSetupRequest calls SetupUserPlane (the gNB
	// always knows the RAN-UE-NGAP-ID at that point, but does not yet know
	// the UE's allocated IP — that's learned lazily from the inner src IP
	// of the first uplink GTP-U packet).
	ueSessions map[int64]*UETunnelSession

	// Hub is the observability hub (nil if not configured).
	Hub *obs.Hub
}

// New creates a new gNB instance ready to connect to the AMF.
func New(cfg Config) *GNB {
	return &GNB{
		config:     cfg,
		setupDone:  make(chan struct{}),
		Hub:        cfg.Hub,
		nextDLTEID: 1,
		uesByRanID: make(map[int64]*UERelayContext),
		ueSessions: make(map[int64]*UETunnelSession),
	}
}

// allocateDLTEID atomically reserves and returns the next DL TEID.
// Ref: TS 29.281 §5.1 — TEID allocation
func (g *GNB) allocateDLTEID() uint32 {
	g.mu.Lock()
	defer g.mu.Unlock()
	teid := g.nextDLTEID
	g.nextDLTEID++
	return teid
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

// UERelayContext tracks a UE connected to the gNB via SCTP.
// Populated in ue_relay.go (Part B).
type UERelayContext struct {
	Conn        net.Conn
	RanUeNgapID int64
	AMFUeNgapID int64
	FirstMsg    bool
}

// UETunnelSession holds per-UE GTP-U state used by the gNB relay.
//
// One entry per active PDU session, keyed by RanUeNgapID in g.ueSessions.
// The relay reads it on every uplink packet (to find the UL TEID + UPF
// address to forward to) and on every downlink packet (to find the UE's
// return UDP address + DL TEID).
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
// Ref: TS 38.401 §8.3 — UE-associated logical NG-connection
type UETunnelSession struct {
	// RanUeNgapID identifies the UE on the N2 interface.
	// Always known to the gNB at PDU Session Resource Setup time.
	// Ref: TS 38.413 §9.3.3.2
	RanUeNgapID int64

	// UEIP is the IP allocated to the UE by the SMF (e.g. 10.45.0.2).
	// Empty until the first uplink packet arrives — the gNB does not see
	// the UE IP in N2 (it lives inside the NAS PDU Session Establishment
	// Accept which the gNB relays opaquely). The UE-facing relay learns
	// it from the inner IPv4 source on first uplink and stores it here.
	UEIP string

	// UESrcAddr is the UE's UDP source address, learned from the first
	// uplink GTP-U packet. Downlink packets are sent back here.
	// nil until the first uplink arrives.
	UESrcAddr *net.UDPAddr

	// ULTEID is the UPF-allocated TEID the gNB uses on uplink (gNB → UPF).
	// Provided via N2 PDU Session Resource Setup (UL NG-U UP TNL Information).
	// Ref: TS 38.413 §9.3.1.9
	ULTEID uint32

	// DLTEID is the gNB-allocated TEID announced to the UPF for downlink
	// (UPF → gNB), and re-used as the TEID toward the UE in this simulator.
	// TODO: split into separate gNB-facing and UE-facing DL TEIDs once the
	// UE simulator supports per-session TEIDs (currently hardcoded to 1).
	DLTEID uint32

	// UPFAddr is the UPF's GTP-U endpoint (ip:port) for the uplink direction.
	UPFAddr *net.UDPAddr

	// upfTunnel is the per-session local socket the gNB uses to talk to the
	// UPF. Bound to a random OS-assigned port in SetupUserPlane so it can
	// coexist with the UPF on the same host. Receives downlink G-PDUs from
	// the UPF on this socket; sends uplink to UPFAddr from it.
	upfTunnel *gtp.Tunnel
}
