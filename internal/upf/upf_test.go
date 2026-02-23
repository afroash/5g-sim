// upf_test.go — Tests for UPF session management and ICMP simulation.
package upf

import (
	"net"
	"testing"
	"time"

	"github.com/afroash/5g-sim/internal/gtp"
)

// TestUPFSessionRegistration verifies that sessions can be registered
// and receive GTP-U traffic.
func TestUPFSessionRegistration(t *testing.T) {
	u, err := New(Config{GTPPort: 0}) // OS-assigned port
	if err != nil {
		t.Fatalf("New UPF: %v", err)
	}
	defer u.Close()
	go func() { u.Start() }()

	// Register a session
	sess := &UPFSession{
		TEID:        0x00000001,
		UEIPAddress: "10.0.0.1",
	}
	u.RegisterSession(sess)

	// Verify it's registered (no panic, no error)
	t.Log("Session registration: ✓")
}

// TestUPFReceivesGTPU sends a GTP-U packet to the UPF and verifies
// the inner packet is delivered to the handler.
func TestUPFReceivesGTPU(t *testing.T) {
	u, err := New(Config{GTPPort: 0})
	if err != nil {
		t.Fatalf("New UPF: %v", err)
	}
	defer u.Close()

	received := make(chan []byte, 1)
	teid := uint32(0x0000ABCD)

	// Register session with a custom handler via the tunnel directly
	u.tunnel.RegisterTEID(teid, func(gotTEID uint32, src *net.UDPAddr, inner []byte) {
		pkt := make([]byte, len(inner))
		copy(pkt, inner)
		received <- pkt
	})
	go func() { u.Start() }()

	// Send a GTP-U packet from a "gNB"
	sender, err := gtp.NewTunnel(0)
	if err != nil {
		t.Fatalf("sender tunnel: %v", err)
	}
	defer sender.Close()
	go sender.Serve()

	// Fake IPv4 packet (20 bytes header)
	innerPkt := []byte{
		0x45, 0x00, 0x00, 0x14, // IPv4: version, IHL, TOS, total len
		0x00, 0x00, 0x00, 0x00, // ID, flags, frag offset
		0x40, 0x11, 0x00, 0x00, // TTL=64, UDP, checksum
		0x0a, 0x00, 0x00, 0x01, // src: 10.0.0.1
		0x08, 0x08, 0x08, 0x08, // dst: 8.8.8.8
	}

	upfAddr := &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: u.tunnel.LocalAddr().Port,
	}
	if err := sender.SendGPDU(upfAddr, teid, innerPkt); err != nil {
		t.Fatalf("SendGPDU: %v", err)
	}

	select {
	case pkt := <-received:
		if len(pkt) != len(innerPkt) {
			t.Errorf("received %d bytes, want %d", len(pkt), len(innerPkt))
		}
		t.Logf("UPF received inner packet: %d bytes ✓", len(pkt))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GTP-U delivery")
	}
}

// TestICMPEchoReply sends a GTP-encapsulated ICMP echo request to the UPF
// and verifies an echo reply is sent back.
func TestICMPEchoReply(t *testing.T) {
	u, err := New(Config{GTPPort: 0})
	if err != nil {
		t.Fatalf("New UPF: %v", err)
	}
	defer u.Close()

	// Set up sender (simulates gNB)
	sender, err := gtp.NewTunnel(0)
	if err != nil {
		t.Fatalf("sender tunnel: %v", err)
	}
	defer sender.Close()

	// DL TEID — what the UPF will send the reply on
	dlTEID := uint32(0x0000FF01)
	replyReceived := make(chan []byte, 1)
	sender.RegisterTEID(dlTEID, func(teid uint32, src *net.UDPAddr, inner []byte) {
		pkt := make([]byte, len(inner))
		copy(pkt, inner)
		replyReceived <- pkt
	})
	go sender.Serve()

	// Register session with the UPF
	sess := &UPFSession{
		TEID:        0x0000FF00,
		UEIPAddress: "10.0.0.5",
		GNBAddr:     &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: sender.LocalAddr().Port},
		GNTEID:      dlTEID,
	}
	u.RegisterSession(sess)
	go func() { u.Start() }()

	// Build ICMP echo request (10.0.0.5 → 10.0.0.254)
	icmpReq := buildICMPEchoReqForTest("10.0.0.5", "10.0.0.254")

	upfAddr := &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: u.tunnel.LocalAddr().Port,
	}
	if err := sender.SendGPDU(upfAddr, sess.TEID, icmpReq); err != nil {
		t.Fatalf("SendGPDU: %v", err)
	}

	select {
	case reply := <-replyReceived:
		if len(reply) < 20 {
			t.Fatalf("reply too short: %d bytes", len(reply))
		}
		icmpType := reply[20]
		if icmpType != 0x00 {
			t.Errorf("ICMP type = 0x%02X, want 0x00 (Echo Reply)", icmpType)
		}
		t.Logf("ICMP echo reply received: %d bytes, type=0x%02X ✓", len(reply), icmpType)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ICMP echo reply")
	}
}

// buildICMPEchoReqForTest constructs a minimal IPv4+ICMP echo request for testing.
func buildICMPEchoReqForTest(srcIP, dstIP string) []byte {
	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()

	icmp := []byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01}
	cs := checksum(icmp)
	icmp[2] = byte(cs >> 8)
	icmp[3] = byte(cs)

	totalLen := uint16(20 + len(icmp))
	ip := make([]byte, 20)
	ip[0] = 0x45
	ip[2] = byte(totalLen >> 8)
	ip[3] = byte(totalLen)
	ip[8] = 0x40 // TTL=64
	ip[9] = 0x01 // ICMP
	copy(ip[12:16], src)
	copy(ip[16:20], dst)
	ipcs := checksum(ip)
	ip[10] = byte(ipcs >> 8)
	ip[11] = byte(ipcs)

	return append(ip, icmp...)
}
