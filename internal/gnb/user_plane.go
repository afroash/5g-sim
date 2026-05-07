// user_plane.go — gNB user plane: GTP-U tunnel to UPF.
//
// After a PDU session is established, the gNB sets up a GTP-U tunnel
// to the UPF. UE uplink traffic is encapsulated and sent to the UPF;
// downlink arrives from the UPF and is delivered to the UE.
//
// In our simulator we:
//  1. Parse UPF tunnel info from the PDU Session Accept
//  2. Create a local GTP-U tunnel
//  3. Send a simulated ping (ICMP echo request) from the UE
//  4. Log the ICMP echo reply that comes back from the UPF
//
// Ref: TS 23.501 §5.8.2.3 — N3 interface (gNB ↔ UPF)
// Ref: TS 29.281 — GTP-U
package gnb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/afroash/5g-sim/internal/gtp"
)

// UserPlane holds the gNB's GTP-U state for one UE session.
//
// In the relay-based model, the UE-facing socket is shared across all UEs
// (g.ueRelay, started at boot). The UPF-facing socket is per-session and
// lives here on Tunnel — it must be a distinct local port from the UPF's
// own socket because gNB and UPF can share a network namespace in the
// simulator's local-host deploy.
type UserPlane struct {
	// Tunnel is the local GTP-U UDP socket (gNB N3 UPF side).
	Tunnel *gtp.Tunnel

	// ULTEID is the TEID we send to the UPF on uplink.
	// (Allocated by the UPF, given to us via SMF/AMF.)
	ULTEID uint32

	// DLTEID is the TEID we tell the UPF to use on downlink.
	// (Allocated by us.)
	DLTEID uint32

	// UPFAddr is the GTP-U endpoint of the UPF.
	UPFAddr *net.UDPAddr

	// UEIPAddress is the UE's allocated IP.
	UEIPAddress string
}

// SetupUserPlane initialises the gNB's user plane state for one PDU session:
//
//   - Opens a per-session UPF-facing GTP-U socket on a random local port
//     (must differ from the UPF's own :2152 because they may share a netns).
//   - Registers the DL-TEID handler that decapsulates UPF→gNB G-PDUs and
//     hands them to the UE-facing relay.
//   - Records the session in g.ueSessions so the UE-facing relay can
//     demux uplinks back to it.
//   - Tells the UPF (via PFCP-sim) where to send downlink packets.
//
// Called from HandlePDUSessionResourceSetupRequest as soon as the UL TEID,
// UPF address and DL TEID are known. The UE IP is *not* known at this
// point — it lives inside the NAS PDU Session Establishment Accept and
// is learned lazily on first uplink (see resolveSessionForUplink).
//
// Ref: TS 23.502 §4.3.2.2.2 — AN specific resource setup
// Ref: TS 29.281 §5.1       — G-PDU forwarding
func (g *GNB) SetupUserPlane(ranUeNgapID int64, ulTEID uint32, upfAddrStr string, dlTEID uint32) (*UserPlane, error) {
	// Parse UPF address ("host:port" or just "host").
	host, portStr, err := net.SplitHostPort(upfAddrStr)
	if err != nil {
		host = upfAddrStr
		portStr = fmt.Sprintf("%d", gtp.GTPUPort)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	upfAddr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}

	// Per-session UPF-facing socket. Random local port so it can coexist
	// with the UPF on the same host.
	tunnel, err := gtp.NewTunnel(0)
	if err != nil {
		return nil, fmt.Errorf("gnb: relay: create UPF-facing GTP-U tunnel: %w", err)
	}

	if g.Hub != nil {
		tunnel.Capture = g.Hub.MakeCaptureFunc("gNB", "UPF")
	}

	// Downlink: UPF sends G-PDUs with this TEID; handleDownlinkPacket
	// finds the session and forwards to the UE on g.ueRelay.
	tunnel.RegisterTEID(dlTEID, g.handleDownlinkPacket)
	go tunnel.Serve()

	up := &UserPlane{
		Tunnel:      tunnel,
		ULTEID:      ulTEID,
		DLTEID:      dlTEID,
		UPFAddr:     upfAddr,
		UEIPAddress: "", // learned later, see resolveSessionForUplink
	}

	// Register with the UE-facing relay. UEIP is intentionally empty here;
	// the relay binds it on first uplink.
	g.registerUESession(&UETunnelSession{
		RanUeNgapID: ranUeNgapID,
		ULTEID:      ulTEID,
		DLTEID:      dlTEID,
		UPFAddr:     upfAddr,
		upfTunnel:   tunnel,
	})

	g.mu.Lock()
	g.userPlane = up
	g.mu.Unlock()

	// PFCP-sim notify: tell the UPF our gNB-side GTP-U source addr/port
	// so it knows where to send downlink replies. The IP is loopback in
	// the local-host deploy because gNB+UPF share the netns.
	gnbGTPAddr := fmt.Sprintf("127.0.0.1:%d", tunnel.LocalAddr().Port)
	if err := notifyUPFOfGNBTunnel(ulTEID, dlTEID, gnbGTPAddr); err != nil {
		fmt.Printf("[gNB] PFCP update to UPF failed: %v\n", err)
	}

	fmt.Printf("[gNB] User plane established:\n")
	fmt.Printf("[gNB]   RAN-UE-NGAP-ID: %d\n", ranUeNgapID)
	fmt.Printf("[gNB]   UPF:            %s\n", upfAddr)
	fmt.Printf("[gNB]   UL TEID:        0x%08X (gNB → UPF)\n", ulTEID)
	fmt.Printf("[gNB]   DL TEID:        0x%08X (UPF → gNB)\n", dlTEID)
	fmt.Printf("[gNB]   gNB GTP-U:      %s\n", gnbGTPAddr)

	return up, nil
}

// handleDownlinkPacket processes an inner IP packet received from the UPF
// on a per-session UPF-facing tunnel and relays it to the UE.
//
// Called from the closure registered in SetupUserPlane against the
// session's DL TEID. It looks the session up by DL TEID (so that this
// path also works if the gNB hosts multiple PDU sessions) and hands off
// to relayDownlinkToUE in ue_gtp_relay.go.
//
// Ref: TS 29.281 §5.1 — G-PDU forwarding
func (g *GNB) handleDownlinkPacket(teid uint32, _ *net.UDPAddr, pkt []byte) {
	if len(pkt) < 20 {
		return
	}
	srcIP := net.IP(pkt[12:16])
	dstIP := net.IP(pkt[16:20])
	protocol := pkt[9]

	protoName := map[uint8]string{0x01: "ICMP", 0x06: "TCP", 0x11: "UDP"}[protocol]
	if protoName == "" {
		protoName = fmt.Sprintf("proto=%d", protocol)
	}

	fmt.Printf("[gNB] ▼ Downlink from UPF: %s → %s (%s) %d bytes via DL-TEID=0x%08X\n",
		srcIP, dstIP, protoName, len(pkt), teid)

	// Surface ICMP echo replies for the simulator's built-in ping test.
	if protocol == 0x01 && len(pkt) >= 28 && pkt[20] == 0x00 {
		fmt.Printf("[gNB] ✓ ICMP echo reply from %s — UPF→gNB leg working\n", srcIP)
	}

	// Relay the inner packet to the UE on the UE-facing socket.
	session := g.lookupUESessionByDLTEID(teid)
	if session == nil {
		fmt.Printf("[gNB] relay: DL dropped — no session for DL-TEID=0x%08X\n", teid)
		return
	}
	g.relayDownlinkToUE(session, pkt)
}

// SendPing sends a simulated ICMP echo request from the UE through the GTP tunnel.
// This exercises the full uplink data path: UE → gNB → GTP-U → UPF.
//
// Ref: RFC 792 — ICMP Echo
func (up *UserPlane) SendPing(dstIP string) error {
	innerPkt := buildICMPEchoRequest(up.UEIPAddress, dstIP, 1, 42)
	if innerPkt == nil {
		return fmt.Errorf("failed to build ICMP echo request")
	}

	fmt.Printf("[gNB] ▲ Uplink: %s → %s (ICMP echo request) via UL-TEID=0x%08X\n",
		up.UEIPAddress, dstIP, up.ULTEID)

	return up.Tunnel.SendGPDU(up.UPFAddr, up.ULTEID, innerPkt)
}

// buildICMPEchoRequest constructs a minimal IPv4+ICMP echo request packet.
// Ref: RFC 791 (IPv4) + RFC 792 (ICMP)
func buildICMPEchoRequest(srcIP, dstIP string, id, seq uint16) []byte {
	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()
	if src == nil || dst == nil {
		return nil
	}

	// ICMP payload: 8 bytes header + 8 bytes data
	icmpPayload := []byte("SIMULATE") // 8 bytes of data
	icmp := make([]byte, 8+len(icmpPayload))
	icmp[0] = 0x08 // type: Echo Request
	icmp[1] = 0x00 // code: 0
	icmp[2] = 0x00 // checksum (fill in)
	icmp[3] = 0x00
	icmp[4] = byte(id >> 8)
	icmp[5] = byte(id)
	icmp[6] = byte(seq >> 8)
	icmp[7] = byte(seq)
	copy(icmp[8:], icmpPayload)

	// ICMP checksum
	cs := internetChecksum(icmp)
	icmp[2] = byte(cs >> 8)
	icmp[3] = byte(cs)

	// IPv4 header (20 bytes)
	totalLen := uint16(20 + len(icmp))
	ip := make([]byte, 20)
	ip[0] = 0x45                // version=4, IHL=5
	ip[1] = 0x00                // DSCP/ECN
	ip[2] = byte(totalLen >> 8) // total length
	ip[3] = byte(totalLen)
	ip[4] = 0x00 // ID
	ip[5] = 0x01
	ip[6] = 0x00 // flags/fragment offset
	ip[7] = 0x00
	ip[8] = 0x40  // TTL = 64
	ip[9] = 0x01  // protocol = ICMP
	ip[10] = 0x00 // checksum (fill in)
	ip[11] = 0x00
	copy(ip[12:16], src) // source IP
	copy(ip[16:20], dst) // destination IP

	// IP header checksum
	ipcs := internetChecksum(ip)
	ip[10] = byte(ipcs >> 8)
	ip[11] = byte(ipcs)

	pkt := append(ip, icmp...)
	return pkt
}

// internetChecksum computes the Internet Checksum (RFC 1071).
func internetChecksum(data []byte) uint16 {
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

// WaitForReply gives the user plane a moment to receive a downlink packet.
func (up *UserPlane) WaitForReply(timeout time.Duration) {
	time.Sleep(timeout)
}

// notifyUPFOfGNBTunnel sends the gNB's GTP-U address and DL TEID to the UPF
// so the UPF knows where to send downlink packets.
// This simulates the gNB-initiated part of PFCP session modification.
func notifyUPFOfGNBTunnel(ulTEID, dlTEID uint32, gnbAddr string) error {
	type patchReq struct {
		ULTEID     uint32 `json:"ulTeid"`
		DLTEID     uint32 `json:"dlTeid"`
		GNBAddress string `json:"gnbAddress"`
	}
	body, _ := json.Marshal(patchReq{ULTEID: ulTEID, DLTEID: dlTEID, GNBAddress: gnbAddr})

	// POST to UPF PFCP-sim to update the session with our downlink address
	url := "http://127.0.0.1:8002/pfcp-sim/v1/sessions"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST UPF PFCP-sim: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[gNB] UPF updated: DL-TEID=0x%08X gNB-GTP=%s\n", dlTEID, gnbAddr)
	return nil
}
