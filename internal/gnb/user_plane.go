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

	// UEFacingTunnel is the GTP-U tunnel for UE→gNB uplink packets.
	// Receives UE uplink, re-encapsulates with UPF UL TEID, forwards to UPF.
	UEFacingTunnel *gtp.Tunnel

	// ueGTPAddr is the UE's GTP endpoint, learned from the first uplink packet.
	ueGTPAddr *net.UDPAddr
}

// SetupUserPlane initialises the gNB's GTP-U tunnel for a UE session.
// Called after PDU Session Establishment Accept is received.
//
// Ref: TS 23.502 §4.3.2.2.2 — AN specific resource setup
func (g *GNB) SetupUserPlane(ueIP string, ulTEID uint32, upfAddrStr string) (*UserPlane, error) {
	// Parse UPF address
	host, portStr, err := net.SplitHostPort(upfAddrStr)
	if err != nil {
		// If no port given, assume default GTP-U port
		host = upfAddrStr
		portStr = fmt.Sprintf("%d", gtp.GTPUPort)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	upfAddr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}

	// Create local GTP-U tunnel (OS-assigned port for gNB)
	tunnel, err := gtp.NewTunnel(0)
	if err != nil {
		return nil, fmt.Errorf("create GTP-U tunnel: %w", err)
	}

	// Use the DL TEID allocated during PDUSessionResourceSetup if available,
	// otherwise fall back to a tunnel-allocated TEID.
	g.mu.RLock()
	dlTEID := g.pendingDLTEID
	g.mu.RUnlock()
	if dlTEID == 0 {
		dlTEID = tunnel.AllocateTEID()
	}

	// Attach capture hook if hub is configured
	if g.Hub != nil {
		tunnel.Capture = g.Hub.MakeCaptureFunc("gNB", "UPF")
		fmt.Println("[gNB] GTP-U packet capture enabled")
	}

	// Register handler for downlink packets from UPF
	tunnel.RegisterTEID(dlTEID, func(teid uint32, src *net.UDPAddr, innerPkt []byte) {
		g.handleDownlinkPacket(teid, src, innerPkt)
	})

	// Start serving in background
	go tunnel.Serve()

	up := &UserPlane{
		Tunnel:      tunnel,
		ULTEID:      ulTEID,
		DLTEID:      dlTEID,
		UPFAddr:     upfAddr,
		UEIPAddress: ueIP,
	}

	// Set up UE-facing GTP-U tunnel (gNB N3 UE side).
	// The UE sends uplink GTP to this port; gNB relays to UPF.
	cfg := g.Config()
	ueTunnel, err := gtp.NewTunnel(cfg.UEGTPPort)
	if err != nil {
		// Non-fatal — UE user plane won't work but signalling still functions.
		fmt.Printf("[gNB] WARNING: UE-facing GTP-U tunnel failed: %v\n", err)
	} else {
		up.UEFacingTunnel = ueTunnel
		// TEID=1 is what the UE sends on its first (and only) PDU session.
		ueTunnel.RegisterTEID(1, func(_ uint32, src *net.UDPAddr, innerPkt []byte) {
			// Learn UE GTP address from first packet.
			if up.ueGTPAddr == nil {
				up.ueGTPAddr = src
				fmt.Printf("[gNB] UE GTP endpoint learned: %s\n", src)
			}
			// Re-encapsulate and forward to UPF.
			if err := tunnel.SendGPDU(up.UPFAddr, up.ULTEID, innerPkt); err != nil {
				fmt.Printf("[gNB] GTP relay UE→UPF error: %v\n", err)
			}
		})
		go ueTunnel.Serve()
		fmt.Printf("[gNB] UE-facing GTP-U tunnel on port %d\n", cfg.UEGTPPort)
	}

	// Store the user plane state so handleDownlinkPacket can relay to UE.
	g.mu.Lock()
	g.userPlane = up
	g.mu.Unlock()

	// Notify UPF of our GTP-U address and DL TEID so it can send downlink back.
	// This is the gNB-side of the PFCP session update.
	gnbGTPAddr := fmt.Sprintf("127.0.0.1:%d", tunnel.LocalAddr().Port)
	if err := notifyUPFOfGNBTunnel(ulTEID, dlTEID, gnbGTPAddr); err != nil {
		fmt.Printf("[gNB] PFCP update to UPF failed: %v\n", err)
		// Non-fatal — uplink still works, downlink may not
	}

	fmt.Printf("[gNB] User plane established:\n")
	fmt.Printf("[gNB]   UE IP:   %s\n", ueIP)
	fmt.Printf("[gNB]   UPF:     %s\n", upfAddr)
	fmt.Printf("[gNB]   UL TEID: 0x%08X (send to UPF)\n", ulTEID)
	fmt.Printf("[gNB]   DL TEID: 0x%08X (UPF sends to us)\n", dlTEID)

	return up, nil
}

// handleDownlinkPacket processes an IP packet received from the UPF
// and delivers it to the UE (logged here since UE is simulated).
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

	fmt.Printf("[gNB] ▼ Downlink: %s → %s (%s) %d bytes via TEID=0x%08X\n",
		srcIP, dstIP, protoName, len(pkt), teid)

	// Check for ICMP echo reply
	if protocol == 0x01 && len(pkt) >= 28 && pkt[20] == 0x00 {
		fmt.Printf("[gNB] ✓ UE received ICMP echo reply from %s — user plane working!\n", srcIP)
	}
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
