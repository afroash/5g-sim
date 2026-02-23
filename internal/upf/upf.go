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
//  3. Simulates a "ping reply" for ICMP echo requests
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
)

// Config holds the UPF startup configuration.
type Config struct {
	// GTPPort is the UDP port for GTP-U (default: 2152).
	GTPPort int

	// BindAddr is the IP address to bind to (default: 0.0.0.0).
	BindAddr string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		GTPPort:  gtp.GTPUPort,
		BindAddr: "0.0.0.0",
	}
}

// UPF is the User Plane Function runtime instance.
type UPF struct {
	config Config
	tunnel *gtp.Tunnel
	mu     sync.Mutex

	// sessions maps TEID → UPF session context (for downlink forwarding)
	sessions map[uint32]*UPFSession
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

	return &UPF{
		config:   cfg,
		tunnel:   tunnel,
		sessions: make(map[uint32]*UPFSession),
	}, nil
}

// RegisterSession tells the UPF about a new UE session.
// Called when a PDU session is established (would normally come via PFCP).
// Ref: TS 29.244 §5 — PFCP session establishment
func (u *UPF) RegisterSession(sess *UPFSession) {
	u.sessions[sess.TEID] = sess
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
// In a real UPF this would be forwarded to the internet via a TUN interface.
// Here we log it and simulate a response for ICMP echo requests.
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

	// Simulate ICMP echo reply if this is a ping
	// Ref: RFC 792 — ICMP
	if protocol == 0x01 && len(pkt) >= 28 {
		icmpType := pkt[20]
		if icmpType == 0x08 { // Echo Request
			fmt.Printf("[UPF] Simulating ICMP echo reply to %s\n", srcIP)
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

// Tunnel returns the UPF's GTP-U tunnel (for tests and TEID allocation).
func (u *UPF) Tunnel() *gtp.Tunnel {
	return u.tunnel
}

// Start begins serving GTP-U packets. Blocks until Close() is called.
func (u *UPF) Start() {
	fmt.Printf("[UPF] Starting — GTP-U on port %d\n", u.config.GTPPort)
	u.tunnel.Serve() // blocks
}

// Close shuts down the UPF's GTP-U tunnel.
func (u *UPF) Close() error {
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
