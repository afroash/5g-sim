// Package upf implements a minimal User Plane Function.
//
// The UPF is the data plane anchor in the 5G core. It:
//   - Receives encapsulated UE packets from the gNB via GTP-U (N3 interface)
//   - Decapsulates and forwards them to the data network (internet)
//   - Re-encapsulates downlink packets back to the gNB
//
// In our simulator the UPF simply:
//  1. Listens for GTP-U packets on UDP port 2152
//  2. Decapsulates them and logs the inner IP packet
//  3. Simulates a "ping reply" for ICMP echo requests (in standalone mode, not sure if we still do this...)
//
// A production UPF would also handle PFCP (N4 interface from SMF)
// for rule installation — we skip that here and use static forwarding.
//
// Ref: TS 23.501 §6.2.3 — UPF
// Ref: TS 29.281 — GTP-U
package upf

import (
	"fmt"
	"net"
	"sync"

	"github.com/afroash/5g-sim/internal/gtp"
	"github.com/afroash/5g-sim/pkg/obs"
)

// Config holds the UPF startup configuration.
type Config struct {
	// GTPPort is the UDP port for GTP-U (default: 2152).
	GTPPort int `yaml:"gtp_port"`

	// BindAddr is the IP address to bind to (default: 0.0.0.0).
	BindAddr string `yaml:"bind_addr"`

	// PFCPSimPort is the HTTP port for the PFCP simulation API.
	// The SMF calls this to register UE sessions.
	// Ref: TS 29.244 §5
	PFCPSimPort int `yaml:"pfcp_sim_port"`

	// N6Iface is the kernel TUN device used for the N6 (data network)
	// reference point. Empty disables N6 forwarding — in that mode the
	// UPF falls back to a fake "ICMP echo reply" used only by unit tests.
	// Ref: TS 23.501 §5.8.2.11.3
	N6Iface string `yaml:"n6_iface"`

	// N6CIDR is the IP/prefix assigned to the N6 TUN. The prefix must
	// cover the UE address pool so the kernel installs a connected route
	// for return traffic. The host IP itself must NOT collide with any UE
	// allocation (the SMF allocates from the low end of the pool starting
	// at network+1), so we sit at the top end. Default: 10.45.0.254/24.
	N6CIDR string `yaml:"n6_cidr"`

	// Hub is the optional observability hub for packet capture.
	Hub *obs.Hub `yaml:"-"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		GTPPort:     gtp.GTPUPort,
		BindAddr:    "0.0.0.0",
		PFCPSimPort: 8002,
		N6Iface:     "upf-n6",
		N6CIDR:      "10.45.0.254/24",
	}
}

// UPF is the User Plane Function runtime instance.
type UPF struct {
	config Config
	tunnel *gtp.Tunnel
	n6     *N6 // nil if N6 forwarding is disabled
	mu     sync.Mutex

	// sessions maps UL TEID → UPF session context (for the GTP-U handler).
	sessions map[uint32]*UPFSession

	// sessionsByUEIP maps UE IP → session, used to route packets that
	// arrive on the N6 TUN (downlink from the data network) back to the
	// correct GTP tunnel toward the gNB.
	sessionsByUEIP map[string]*UPFSession
}

// UPFSession tracks a single UE's user plane state.
type UPFSession struct {
	TEID        uint32       // Uplink TEID (what gNB sends us)
	GNBAddr     *net.UDPAddr // gNB's GTP-U address
	GNTEID      uint32       // Downlink TEID (what we send to gNB)
	UEIPAddress string       // UE's allocated IP
}

// New creates a new UPF instance.
func New(cfg Config) (*UPF, error) {
	tunnel, err := gtp.NewTunnel(cfg.GTPPort)
	if err != nil {
		return nil, fmt.Errorf("create GTP-U tunnel: %w", err)
	}

	u := &UPF{
		config:         cfg,
		tunnel:         tunnel,
		sessions:       make(map[uint32]*UPFSession),
		sessionsByUEIP: make(map[string]*UPFSession),
	}

	// Attach capture hook if hub is configured
	if cfg.Hub != nil {
		tunnel.Capture = cfg.Hub.MakeCaptureFunc("UPF", "gNB")
		fmt.Println("[UPF] GTP-U packet capture enabled")
	}

	return u, nil
}

// RegisterSession tells the UPF about a new UE session.
// Called when a PDU session is established (would normally come via PFCP).
// Ref: TS 29.244 §5 — PFCP session establishment
func (u *UPF) RegisterSession(sess *UPFSession) {
	u.mu.Lock()
	u.sessions[sess.TEID] = sess
	if sess.UEIPAddress != "" {
		u.sessionsByUEIP[sess.UEIPAddress] = sess
	}
	u.mu.Unlock()
	u.tunnel.RegisterTEID(sess.TEID, u.makeHandler(sess))
	fmt.Printf("[UPF] Session registered: TEID=0x%08X UE=%s gNB=%s\n",
		sess.TEID, sess.UEIPAddress, sess.GNBAddr)
}

// makeHandler returns a GTP-U handler for a specific UE session.
func (u *UPF) makeHandler(sess *UPFSession) gtp.HandlerFunc {
	return func(teid uint32, src *net.UDPAddr, innerPkt []byte) {
		u.handleUplinkPacket(sess, src, innerPkt)
	}
}

// handleUplinkPacket processes a decapsulated packet from the gNB.
//
// When the N6 TUN is up (production deploy) the inner packet is written
// to the TUN and the kernel routes it via OSPF default route to the data
// network (internet-sim). When N6 is unavailable (unit tests, no
// NET_ADMIN), the legacy "fake ICMP echo reply" path runs as a fallback
// so existing tests continue to pass.
//
// Ref: TS 23.501 §5.8.2.11.3 — N6
// Ref: TS 29.281 §5.1        — G-PDU forwarding
func (u *UPF) handleUplinkPacket(sess *UPFSession, src *net.UDPAddr, pkt []byte) {
	if len(pkt) < 20 {
		fmt.Printf("[UPF] Uplink packet too short: %d bytes\n", len(pkt))
		return
	}

	// Parse IPv4 header basics
	srcIP := net.IP(pkt[12:16])
	dstIP := net.IP(pkt[16:20])
	protocol := pkt[9]

	protoName := map[uint8]string{
		0x01: "ICMP",
		0x06: "TCP",
		0x11: "UDP",
	}[protocol]
	if protoName == "" {
		protoName = fmt.Sprintf("proto=%d", protocol)
	}

	fmt.Printf("[UPF] ▲ Uplink: %s → %s (%s) %d bytes via TEID=0x%08X\n",
		srcIP, dstIP, protoName, len(pkt), sess.TEID)

	// Production path: hand the packet to the kernel via the N6 TUN.
	// The kernel's normal forwarding rules take it from there.
	if u.n6 != nil {
		if err := u.n6.Inject(pkt); err != nil {
			fmt.Printf("[UPF] N6 inject error: %v\n", err)
		}
		return
	}

	// Fallback for unit tests: simulate an ICMP echo reply.
	// Ref: RFC 792 — ICMP
	if protocol == 0x01 && len(pkt) >= 28 {
		icmpType := pkt[20]
		if icmpType == 0x08 { // Echo Request
			fmt.Printf("[UPF] (no N6) simulating ICMP echo reply to %s\n", srcIP)
			reply := buildICMPEchoReply(pkt)
			if reply != nil && sess.GNBAddr != nil {
				if err := u.tunnel.SendGPDU(sess.GNBAddr, sess.GNTEID, reply); err != nil {
					fmt.Printf("[UPF] Failed to send downlink: %v\n", err)
				} else {
					fmt.Printf("[UPF] ▼ Downlink: %s → %s (ICMP reply) via TEID=0x%08X\n",
						dstIP, srcIP, sess.GNTEID)
				}
			}
		}
	}
}

// handleN6Packet processes a packet read from the N6 TUN (return traffic
// from the data network). It looks the session up by inner destination IP
// and forwards the packet to the gNB encapsulated with the session's DL
// TEID.
//
// Ref: TS 29.281 §5.1 — G-PDU
func (u *UPF) handleN6Packet(pkt []byte) {
	if len(pkt) < 20 {
		return
	}
	// IPv4 only for now.
	if pkt[0]>>4 != 4 {
		return
	}
	srcIP := net.IP(pkt[12:16])
	dstIP := net.IP(pkt[16:20]).String()

	u.mu.Lock()
	sess := u.sessionsByUEIP[dstIP]
	u.mu.Unlock()
	if sess == nil {
		fmt.Printf("[UPF] N6 drop: no session for dst %s (src=%s)\n", dstIP, srcIP)
		return
	}
	if sess.GNBAddr == nil {
		fmt.Printf("[UPF] N6 drop: session for %s has no gNB addr yet\n", dstIP)
		return
	}

	if err := u.tunnel.SendGPDU(sess.GNBAddr, sess.GNTEID, pkt); err != nil {
		fmt.Printf("[UPF] N6→gNB send error: %v\n", err)
		return
	}
	fmt.Printf("[UPF] ▼ Downlink: %s → %s (%d bytes) via DL-TEID=0x%08X\n",
		srcIP, dstIP, len(pkt), sess.GNTEID)
}

// Tunnel returns the UPF's GTP-U tunnel (for tests and TEID allocation).
func (u *UPF) Tunnel() *gtp.Tunnel {
	return u.tunnel
}

// Start begins serving GTP-U packets and (if configured) the N6 read loop.
// Blocks until Close() is called.
func (u *UPF) Start() {
	// Best-effort N6 setup. Failures are non-fatal so unit tests without
	// NET_ADMIN still run — the legacy fake-reply path takes over.
	if u.config.N6Iface != "" {
		n6, err := StartN6(u.config.N6Iface, u.config.N6CIDR)
		if err != nil {
			fmt.Printf("[UPF] N6 disabled: %v\n", err)
			// note: when the upf is running standalone, this will not be able to setup the N6 interface, this is ok, we will use the fake-reply path.	
			// TODO: we need to handle this case, similar to the ue. we want to be able to visually see requests going to and from upf towards the data network over the N6. 
			// as it would happen over a real network. 
		} else {
			u.n6 = n6
			go n6.ReadLoop(u.handleN6Packet)
		}
	}

	fmt.Printf("[UPF] Starting — GTP-U on port %d\n", u.config.GTPPort)
	u.tunnel.Serve() // blocks
}

// Close shuts down the UPF's GTP-U tunnel and N6 TUN (if open).
func (u *UPF) Close() error {
	if u.n6 != nil {
		u.n6.Close()
	}
	return u.tunnel.Close()
}

// --- ICMP helpers ---

// buildICMPEchoReply constructs an ICMP Echo Reply from an Echo Request.
// Swaps src/dst, changes ICMP type 8→0, recalculates checksums.
// Ref: RFC 792 §ECHO
func buildICMPEchoReply(request []byte) []byte {
	if len(request) < 28 {
		return nil
	}

	reply := make([]byte, len(request))
	copy(reply, request)

	// Swap source and destination IP
	copy(reply[12:16], request[16:20]) // src ← orig dst
	copy(reply[16:20], request[12:16]) // dst ← orig src

	// Change ICMP type: 0x08 (Echo Request) → 0x00 (Echo Reply)
	reply[20] = 0x00

	// Recalculate ICMP checksum (bytes 22-23)
	reply[22] = 0x00
	reply[23] = 0x00
	icmpChecksum := checksum(reply[20:])
	reply[22] = byte(icmpChecksum >> 8)
	reply[23] = byte(icmpChecksum)

	// Recalculate IP header checksum (bytes 10-11)
	reply[10] = 0x00
	reply[11] = 0x00
	ipChecksum := checksum(reply[:20])
	reply[10] = byte(ipChecksum >> 8)
	reply[11] = byte(ipChecksum)

	return reply
}

// checksum computes the Internet checksum (RFC 1071).
func checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}
