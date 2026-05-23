// ue_gtp_relay.go — gNB GTP-U relay for the UE side of N3.
//
// In a real 5G deployment, the gNB sits between the UE (radio) and the UPF
// (core). This simulator models that with two GTP-U tunnels on the gNB:
//
//	UE → gNB:2153 (GTP-U)        — UE-facing socket (started here, at boot)
//	gNB → UPF:2152 (GTP-U)       — UPF-facing socket (per-session, see user_plane.go)
//
// Uplink (UE → UPF):
//  1. UE sends an inner IP packet GTP-U-encapsulated to gNB:2153.
//  2. handleUplinkFromUE strips GTP, looks up the session by inner src IP,
//     caches the UE's UDP src addr (the return path), and re-encapsulates
//     the packet with the session's UL TEID toward the UPF.
//
// Downlink (UPF → UE):
//  1. UPF sends a GTP-U packet to the gNB's per-session UPF-facing socket
//     (registered via PFCP-sim notify).
//  2. handleDownlinkPacket (in user_plane.go) finds the session by DL TEID
//     and calls relayDownlinkToUE here.
//  3. relayDownlinkToUE re-encapsulates with the DL TEID and sends out
//     the UE-facing socket to the UE's cached return address.
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
// Ref: TS 38.401 §8.3 — gNB user plane functions
package gnb

import (
	"fmt"
	"net"

	"github.com/afroash/5g-sim/internal/gtp"
	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// startUEGTPRelay binds the UE-facing GTP-U socket and starts its read loop.
//
// One socket serves all UEs attached to this gNB; uplink packets are
// demultiplexed in handleUplinkFromUE by inner source IP. The current UE
// simulator hardcodes TEID=1 in both directions (see internal/ue/tun.go),
// so we register a single TEID=1 handler here. When the UE simulator
// supports per-session TEIDs, this will need to be widened to a wildcard
// or per-session registration.
//
// Ref: TS 29.281 §4.4.2 — UDP/IP based transport
// Ref: TS 29.281 §5.1   — G-PDU forwarding
func (g *GNB) startUEGTPRelay(port int) error {
	tunnel, err := gtp.NewTunnel(port)
	if err != nil {
		return fmt.Errorf("gnb: relay: bind UE-facing GTP-U on port %d: %w", port, err)
	}

	// Optional packet capture, mirroring the UPF-facing tunnel hook.
	if g.Hub != nil {
		tunnel.Capture = g.Hub.MakeCaptureFunc("UE", "gNB")
	}

	// UEs may use TEID=1 (PDU session id) or per-session TEIDs; demux is by UDP src.
	tunnel.RegisterDefaultHandler(g.handleUplinkFromUE)

	g.mu.Lock()
	g.ueRelay = tunnel
	g.mu.Unlock()

	go tunnel.Serve()
	fmt.Printf("[gNB] UE-facing GTP-U relay listening on port %d\n", port)
	return nil
}

// handleUplinkFromUE is the TEID handler for packets arriving on the
// UE-facing socket.
//
// Sessions are looked up by the UE's UDP source address (the GTP-U socket
// the UE writes from). That binding is stable for the lifetime of the
// PDU session and is unaffected by inner-packet anomalies (IPv6 traffic
// the kernel emits on the TUN, packets with mgmt-network source IPs the
// kernel happens to route via the default route, etc.). The session is
// claimed on the first valid IPv4 packet from a previously-unseen UDP
// source address.
//
// Inbound packets are filtered to plausible IPv4 G-PDUs before any session
// state is touched, otherwise the very first kernel-generated IPv6 RS on
// the UE's TUN would mis-bind the session.
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
func (g *GNB) handleUplinkFromUE(teid uint32, src *net.UDPAddr, innerPkt []byte) {
	if !plausibleUEIPv4(innerPkt) {
		// Silent drop — logging every IPv6 RS / multicast packet would
		// flood the gNB log on TUN bring-up.
		return
	}
	srcIP := net.IP(innerPkt[12:16]).String()

	session := g.resolveSessionForUplink(src, srcIP)
	if session == nil {
		fmt.Printf("[gNB] relay: UE uplink dropped — no session for UDP src %s (inner src %s, TEID=0x%08X)\n",
			src, srcIP, teid)
		return
	}

	if session.upfTunnel == nil || session.UPFAddr == nil {
		fmt.Printf("[gNB] relay: UE uplink dropped — UPF tunnel not ready for %s\n", srcIP)
		return
	}

	if err := session.upfTunnel.SendGPDU(session.UPFAddr, session.ULTEID, innerPkt); err != nil {
		fmt.Printf("[gNB] relay: UE→UPF send error for %s: %v\n", srcIP, err)
		return
	}

	dstIP := net.IP(innerPkt[16:20])
	fmt.Printf("[gNB] ▲ Uplink relayed: %s → %s (%d bytes) UL-TEID=0x%08X\n",
		srcIP, dstIP, len(innerPkt), session.ULTEID)
	g.emitGTPPacket(seqdiag.NodeUE, seqdiag.NodeUPF, innerPkt, session.ULTEID)
}

// plausibleUEIPv4 returns true iff pkt looks like an IPv4 G-PDU that
// could plausibly be sourced by a UE: long enough, version 4, and a
// non-zero, non-multicast, non-broadcast source address. The aim is to
// reject kernel-generated noise (IPv6 RS/NS, multicast listener reports,
// DAD probes from src 0.0.0.0) without trying to validate the source
// against any specific UE pool.
func plausibleUEIPv4(pkt []byte) bool {
	if len(pkt) < 20 {
		return false
	}
	if pkt[0]>>4 != 4 {
		return false
	}
	src0 := pkt[12]
	if src0 == 0 || src0 == 255 || src0 >= 224 {
		return false // 0.0.0.0/8, multicast 224.0.0.0/4, broadcast
	}
	return true
}

// resolveSessionForUplink finds the session whose cached UE UDP source
// address matches udpSrc. If none matches and an unbound session exists
// (created at N2 time, no uplink seen yet), it is claimed and bound to
// this UDP source. Returns nil if no slot is available.
//
// Ref: TS 29.281 §5.1
func (g *GNB) resolveSessionForUplink(udpSrc *net.UDPAddr, innerSrcIP string) *UETunnelSession {
	udpKey := udpSrc.String()

	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. Match by previously-seen UE UDP source addr.
	for _, s := range g.ueSessions {
		if s.UESrcAddr != nil && s.UESrcAddr.String() == udpKey {
			// Opportunistically populate UEIP for logging the first time
			// we see a clean IPv4 packet on this session.
			if s.UEIP == "" {
				s.UEIP = innerSrcIP
			}
			return s
		}
	}

	// 2. Claim an unbound session (no uplink yet). Single-UE shortcut.
	for _, s := range g.ueSessions {
		if s.UESrcAddr == nil {
			s.UESrcAddr = udpSrc
			s.UEIP = innerSrcIP
			fmt.Printf("[gNB] relay: bound RAN-UE-NGAP-ID=%d to UDP src %s (UE IP %s)\n",
				s.RanUeNgapID, udpSrc, innerSrcIP)
			return s
		}
	}
	return nil
}

// relayDownlinkToUE re-encapsulates an inner IP packet (already decapsulated
// from the UPF-facing GTP-U socket) and sends it to the UE on the UE-facing
// socket using the session's DL TEID.
//
// Called from handleDownlinkPacket in user_plane.go after it has resolved
// the session from the incoming DL TEID.
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
func (g *GNB) relayDownlinkToUE(session *UETunnelSession, innerPkt []byte) {
	g.mu.RLock()
	relay := g.ueRelay
	g.mu.RUnlock()

	if relay == nil {
		fmt.Printf("[gNB] relay: DL dropped — UE-facing GTP-U not started\n")
		return
	}

	g.mu.RLock()
	ueAddr := session.UESrcAddr
	g.mu.RUnlock()

	if ueAddr == nil {
		// No uplink yet → we don't know where to send the reply.
		fmt.Printf("[gNB] relay: DL dropped — no UE return address for %s yet\n",
			session.UEIP)
		return
	}

	if err := relay.SendGPDU(ueAddr, session.DLTEID, innerPkt); err != nil {
		fmt.Printf("[gNB] relay: gNB→UE send error for %s: %v\n", session.UEIP, err)
		return
	}
	fmt.Printf("[gNB] ▼ Downlink relayed to UE %s via DL-TEID=0x%08X (%d bytes)\n",
		session.UEIP, session.DLTEID, len(innerPkt))
	g.emitGTPPacket(seqdiag.NodeUPF, seqdiag.NodeUE, innerPkt, session.DLTEID)
}

func (g *GNB) emitGTPPacket(from, to seqdiag.Node, pkt []byte, teid uint32) {
	if len(pkt) < 20 {
		return
	}
	src := net.IP(pkt[12:16])
	dst := net.IP(pkt[16:20])
	summary := fmt.Sprintf("%s → %s (%d bytes)", src, dst, len(pkt))
	obspub.EmitPacket(from, to, "gtp", summary, "TS 29.281 §5.1", map[string]string{
		"teid": fmt.Sprintf("0x%08X", teid),
		"len":  fmt.Sprintf("%d", len(pkt)),
	})
}

// registerUESession adds a session to the relay's lookup map.
// Called from SetupUserPlane once UL TEID, DL TEID, UPF address and the
// per-session UPF-facing tunnel are all known.
//
// At call time UEIP is typically empty — it's filled in lazily by
// resolveSessionForUplink when the UE's first uplink packet arrives.
//
// Ref: TS 29.281 §5.1
func (g *GNB) registerUESession(s *UETunnelSession) {
	g.mu.Lock()
	g.ueSessions[s.RanUeNgapID] = s
	g.mu.Unlock()
	fmt.Printf("[gNB] relay: session registered — RAN-UE-NGAP-ID=%d UL-TEID=0x%08X DL-TEID=0x%08X UPF=%s\n",
		s.RanUeNgapID, s.ULTEID, s.DLTEID, s.UPFAddr)
}

// lookupUESessionByDLTEID returns the session whose DL TEID matches teid,
// or nil if none. Used by the UPF-facing tunnel handler to route a
// downlink G-PDU back to the right UE.
//
// Ref: TS 29.281 §5.1
func (g *GNB) lookupUESessionByDLTEID(teid uint32) *UETunnelSession {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, s := range g.ueSessions {
		if s.DLTEID == teid {
			return s
		}
	}
	return nil
}
