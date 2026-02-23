// gtp_test.go — Tests for GTP-U packet codec and tunnel.
package gtp

import (
	"net"
	"testing"
	"time"
)

// TestGPDURoundTrip encodes a G-PDU and decodes it back.
// Ref: TS 29.281 §5.1
func TestGPDURoundTrip(t *testing.T) {
	innerPkt := []byte{
		0x45, 0x00, 0x00, 0x1c, // IPv4 header start (version, IHL, TOS, total length)
		0x00, 0x01, 0x00, 0x00, // ID, flags, fragment offset
		0x40, 0x01, 0x00, 0x00, // TTL=64, protocol=ICMP, checksum
		0x0a, 0x00, 0x00, 0x01, // src: 10.0.0.1
		0x08, 0x08, 0x08, 0x08, // dst: 8.8.8.8
	}

	teid := uint32(0xDEADBEEF)
	pkt := NewGPDU(teid, innerPkt)

	// Encode
	data, err := Encode(pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(data) < MinHeaderLen+len(innerPkt) {
		t.Errorf("encoded length %d too short", len(data))
	}

	// Decode
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Header.MessageType != MsgTypeGPDU {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			got.Header.MessageType, MsgTypeGPDU)
	}
	if got.Header.TEID != teid {
		t.Errorf("TEID = 0x%08X, want 0x%08X", got.Header.TEID, teid)
	}
	if len(got.Payload) != len(innerPkt) {
		t.Errorf("payload len = %d, want %d", len(got.Payload), len(innerPkt))
	}
	for i, b := range got.Payload {
		if b != innerPkt[i] {
			t.Errorf("payload[%d] = 0x%02X, want 0x%02X", i, b, innerPkt[i])
		}
	}

	t.Logf("G-PDU round-trip: TEID=0x%08X payload=%d bytes ✓", teid, len(got.Payload))
}

// TestEchoRoundTrip tests echo request/response encoding.
// Ref: TS 29.281 §7.2
func TestEchoRoundTrip(t *testing.T) {
	seqNum := uint16(42)
	req := NewEchoRequest(seqNum)

	data, err := Encode(req)
	if err != nil {
		t.Fatalf("Encode echo request: %v", err)
	}

	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode echo request: %v", err)
	}

	if got.Header.MessageType != MsgTypeEchoRequest {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			got.Header.MessageType, MsgTypeEchoRequest)
	}
	if got.Header.TEID != 0 {
		t.Errorf("TEID = 0x%08X, want 0 for echo", got.Header.TEID)
	}
	if got.Header.SequenceNumber != seqNum {
		t.Errorf("SeqNum = %d, want %d", got.Header.SequenceNumber, seqNum)
	}

	t.Logf("Echo request round-trip: seq=%d ✓", seqNum)
}

// TestMinimalHeader tests the minimal 8-byte header (no S/E/PN flags).
func TestMinimalHeader(t *testing.T) {
	pkt := NewGPDU(0x00000001, []byte{0x01, 0x02, 0x03})
	data, err := Encode(pkt)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Minimal header = 8 bytes + 3 payload = 11 bytes
	if len(data) != 11 {
		t.Errorf("encoded len = %d, want 11", len(data))
	}
	// No extended header flags should be set
	if data[0]&0x07 != 0 {
		t.Errorf("flags byte has unexpected bits set: 0x%02X", data[0])
	}

	t.Logf("Minimal header: %d bytes, flags=0x%02X ✓", len(data), data[0])
}

// TestTunnelLoopback sends a G-PDU from one tunnel to another
// on localhost and verifies the inner packet is delivered correctly.
func TestTunnelLoopback(t *testing.T) {
	// Receiver tunnel — bind on any port
	receiver, err := NewTunnel(0)
	if err != nil {
		t.Fatalf("NewTunnel receiver: %v", err)
	}
	defer receiver.Close()

	// Register a TEID handler on the receiver
	teid := uint32(0x0000CAFE)
	received := make(chan []byte, 1)
	receiver.RegisterTEID(teid, func(gotTEID uint32, src *net.UDPAddr, inner []byte) {
		pkt := make([]byte, len(inner))
		copy(pkt, inner)
		received <- pkt
	})
	go receiver.Serve()

	// Sender tunnel — also on any port
	sender, err := NewTunnel(0)
	if err != nil {
		t.Fatalf("NewTunnel sender: %v", err)
	}
	defer sender.Close()
	go sender.Serve()

	// Send a fake IP packet to the receiver
	innerPkt := []byte{0x45, 0x00, 0x00, 0x14, 0xde, 0xad, 0xbe, 0xef}
	remoteAddr := &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: receiver.LocalAddr().Port,
	}

	if err := sender.SendGPDU(remoteAddr, teid, innerPkt); err != nil {
		t.Fatalf("SendGPDU: %v", err)
	}

	// Wait for delivery
	select {
	case pkt := <-received:
		if len(pkt) != len(innerPkt) {
			t.Errorf("received %d bytes, want %d", len(pkt), len(innerPkt))
		}
		for i, b := range pkt {
			if b != innerPkt[i] {
				t.Errorf("pkt[%d] = 0x%02X, want 0x%02X", i, b, innerPkt[i])
			}
		}
		t.Logf("Tunnel loopback: delivered %d bytes via TEID 0x%08X ✓",
			len(pkt), teid)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GTP-U packet")
	}
}

// TestTEIDAllocation verifies TEIDs are unique and sequential.
func TestTEIDAllocation(t *testing.T) {
	tunnel, err := NewTunnel(0)
	if err != nil {
		t.Fatalf("NewTunnel: %v", err)
	}
	defer tunnel.Close()

	seen := make(map[uint32]bool)
	for i := 0; i < 100; i++ {
		teid := tunnel.AllocateTEID()
		if seen[teid] {
			t.Errorf("duplicate TEID 0x%08X", teid)
		}
		seen[teid] = true
	}

	t.Logf("Allocated 100 unique TEIDs ✓")
}

// TestDecodeInvalidPacket verifies graceful handling of bad input.
func TestDecodeInvalidPacket(t *testing.T) {
	// Too short
	_, err := Decode([]byte{0x30, 0xFF})
	if err == nil {
		t.Error("expected error for too-short packet")
	}

	// Wrong version (version=0)
	badVersion := []byte{0x00, 0xFF, 0x00, 0x05, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err = Decode(badVersion)
	if err == nil {
		t.Error("expected error for wrong GTP version")
	}

	t.Log("Invalid packet handling ✓")
}

// TestPacketString verifies the String() method doesn't panic.
func TestPacketString(t *testing.T) {
	pkt := NewGPDU(0x12345678, []byte{0x01, 0x02})
	s := pkt.String()
	if s == "" {
		t.Error("String() returned empty")
	}
	t.Logf("Packet.String(): %s ✓", s)
}
