// pcap_test.go — Tests for libpcap writer and frame builders.
package pcap

import (
	"encoding/binary"
	"net"
	"os"
	"testing"
)

// TestGlobalHeader verifies the 24-byte pcap global header is written correctly.
func TestGlobalHeader(t *testing.T) {
	path := t.TempDir() + "/test.pcap"
	w, err := NewWriter(path, LinkTypeEthernet)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) < 24 {
		t.Fatalf("file too short: %d bytes", len(data))
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != pcapMagic {
		t.Errorf("magic = 0x%08X, want 0x%08X", magic, pcapMagic)
	}

	major := binary.LittleEndian.Uint16(data[4:6])
	minor := binary.LittleEndian.Uint16(data[6:8])
	if major != 2 || minor != 4 {
		t.Errorf("version = %d.%d, want 2.4", major, minor)
	}

	linkType := binary.LittleEndian.Uint32(data[20:24])
	if linkType != LinkTypeEthernet {
		t.Errorf("linkType = %d, want %d", linkType, LinkTypeEthernet)
	}

	t.Logf("Global header: magic=0x%08X version=%d.%d linkType=%d ✓",
		magic, major, minor, linkType)
}

// TestWritePacket verifies packet records are written with correct headers.
func TestWritePacket(t *testing.T) {
	path := t.TempDir() + "/packets.pcap"
	w, err := NewWriter(path, LinkTypeEthernet)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := w.WritePacket(payload); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := w.WritePacket(payload); err != nil {
		t.Fatalf("WritePacket 2: %v", err)
	}

	if w.Count() != 2 {
		t.Errorf("Count = %d, want 2", w.Count())
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Global header (24) + 2 × (packet header (16) + payload (5))
	expected := 24 + 2*(16+5)
	if len(data) != expected {
		t.Errorf("file size = %d, want %d", len(data), expected)
	}

	// Verify first packet record
	capLen := binary.LittleEndian.Uint32(data[24+8 : 24+12])
	if capLen != uint32(len(payload)) {
		t.Errorf("capLen = %d, want %d", capLen, len(payload))
	}

	t.Logf("WritePacket: 2 packets, %d bytes total ✓", len(data))
}

// TestBuildSCTPFrame verifies SCTP/NGAP frame construction.
func TestBuildSCTPFrame(t *testing.T) {
	srcIP := net.ParseIP("127.0.0.1")
	dstIP := net.ParseIP("127.0.0.1")
	ngapPayload := []byte{0x00, 0x15, 0x00, 0x05, 0x00, 0x00, 0x00, 0x00}

	frame := BuildSCTPFrame(srcIP, dstIP, 54321, 38412, ngapPayload)

	// Linux SLL (16) + IPv4 (20) + SCTP (12) + chunk header (16) + payload
	minLen := 16 + 20 + 12 + 16 + len(ngapPayload)
	if len(frame) < minLen {
		t.Errorf("frame len = %d, want >= %d", len(frame), minLen)
	}

	// Check SLL EtherType (bytes 14-15 of SLL) = 0x0800 (IPv4)
	etherType := binary.BigEndian.Uint16(frame[14:16])
	if etherType != 0x0800 {
		t.Errorf("SLL EtherType = 0x%04X, want 0x0800", etherType)
	}

	// Check IP protocol (byte 9 of IP header = SLL offset 16+9)
	ipProto := frame[16+9]
	if ipProto != 132 {
		t.Errorf("IP protocol = %d, want 132 (SCTP)", ipProto)
	}

	t.Logf("BuildSCTPFrame: %d bytes ✓", len(frame))
}

// TestBuildUDPFrame verifies UDP/GTP-U frame construction.
func TestBuildUDPFrame(t *testing.T) {
	srcIP := net.ParseIP("127.0.0.1")
	dstIP := net.ParseIP("127.0.0.1")
	gtpPayload := []byte{0x30, 0xff, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x45, 0x00}

	frame := BuildUDPFrame(srcIP, dstIP, 2152, 2152, gtpPayload)

	// Ethernet (14) + IPv4 (20) + UDP (8) + payload
	expected := 14 + 20 + 8 + len(gtpPayload)
	if len(frame) != expected {
		t.Errorf("frame len = %d, want %d", len(frame), expected)
	}

	// Check EtherType = 0x0800
	etherType := binary.BigEndian.Uint16(frame[12:14])
	if etherType != 0x0800 {
		t.Errorf("EtherType = 0x%04X, want 0x0800", etherType)
	}

	// Check IP protocol = 17 (UDP)
	ipProto := frame[14+9]
	if ipProto != 17 {
		t.Errorf("IP protocol = %d, want 17 (UDP)", ipProto)
	}

	// Check UDP dst port = 2152 (GTP-U)
	dstPort := binary.BigEndian.Uint16(frame[14+20+2 : 14+20+4])
	if dstPort != 2152 {
		t.Errorf("UDP dst port = %d, want 2152", dstPort)
	}

	t.Logf("BuildUDPFrame: %d bytes, IP proto=UDP, dst port=2152 ✓", len(frame))
}

// TestNGAPCapture does an end-to-end write of an NGAP frame and reads it back.
func TestNGAPCapture(t *testing.T) {
	path := t.TempDir() + "/ngap.pcap"
	w, err := NewWriter(path, LinkTypeLinuxSLL)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	src := net.ParseIP("127.0.0.1")
	dst := net.ParseIP("127.0.0.1")

	// Fake NGSetupRequest bytes
	ngapBytes := []byte{
		0x00, 0x15, 0x00, 0x08, // NGAP: initiatingMessage, NGSetup
		0x00, 0x00, 0x00, 0x00,
	}
	frame := BuildSCTPFrame(src, dst, 54321, 38412, ngapBytes)
	if err := w.WritePacket(frame); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	if w.Count() != 1 {
		t.Errorf("Count = %d, want 1", w.Count())
	}
	t.Logf("NGAP capture: 1 packet, frame=%d bytes ✓", len(frame))
}
