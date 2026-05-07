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

	// TEID=1 is what the UE simulator sends on; see internal/ue/tun.go.
	// TODO(multi-UE): allocate per-UE TEIDs when the UE side learns its TEID
	// from the PDU Session Establishment Accept.
	tunnel.RegisterTEID(1, g.handleUplinkFromUE)

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
// Lookup strategy: the gNB does not know the UE IP at PDU Session Resource
// Setup time (it lives inside the NAS PDU Session Establishment Accept,
// which the gNB relays opaquely). The session is therefore created with
// UEIP = "" and the relay learns the IP from the first uplink packet:
//
//  1. Find a session whose cached UEIP already matches the inner src IP.
//  2. Failing that, claim a session with UEIP == "" and assign srcIP to it.
//  3. Failing that, drop the packet.
//
// Step 2 is a single-UE shortcut: with multiple concurrent UEs we'd need
// per-UE TEIDs (already on the TODO list). The current UE simulator hard-
// codes TEID=1 so demux by TEID alone is impossible.
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
func (g *GNB) handleUplinkFromUE(teid uint32, src *net.UDPAddr, innerPkt []byte) {
	if len(innerPkt) < 20 {
		fmt.Printf("[gNB] relay: UE uplink dropped — inner packet too short (%d bytes)\n",
			len(innerPkt))
		return
	}
	srcIP := net.IP(innerPkt[12:16]).String()

	session := g.resolveSessionForUplink(srcIP)
	if session == nil {
		fmt.Printf("[gNB] relay: UE uplink dropped — no session for UE IP %s (TEID=0x%08X)\n",
			srcIP, teid)
		return
	}

	// Cache the UE's return address on the first uplink packet so the
	// downlink path knows where to send replies.
	g.mu.Lock()
	if session.UESrcAddr == nil {
		session.UESrcAddr = src
		fmt.Printf("[gNB] relay: UE GTP endpoint learned for %s: %s\n", srcIP, src)
	} else if session.UESrcAddr.String() != src.String() {
		// UE roamed source port — refresh.
		session.UESrcAddr = src
	}
	g.mu.Unlock()

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
}

// resolveSessionForUplink finds the session matching the inner src IP,
// or claims an unbound session (UEIP == "") and assigns srcIP to it.
// Returns nil if no session is available.
//
// Ref: TS 29.281 §5.1
func (g *GNB) resolveSessionForUplink(srcIP string) *UETunnelSession {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. Exact match on already-known UE IP.
	for _, s := range g.ueSessions {
		if s.UEIP == srcIP {
			return s
		}
	}

	// 2. Claim an unbound session (created at N2 time, not yet seen any
	// uplink). Single-UE shortcut.
	for _, s := range g.ueSessions {
		if s.UEIP == "" {
			s.UEIP = srcIP
			fmt.Printf("[gNB] relay: bound RAN-UE-NGAP-ID=%d to UE IP %s\n",
				s.RanUeNgapID, srcIP)
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
