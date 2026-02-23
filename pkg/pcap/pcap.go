// Package pcap writes libpcap-format capture files.
//
// The libpcap format is understood by Wireshark, tcpdump, and every
// other packet analysis tool. By writing our NGAP and GTP-U traffic
// in this format we get full protocol dissection for free — Wireshark
// has native dissectors for both protocols.
//
// File format (RFC 2149 / libpcap):
//
//	Global header (24 bytes)
//	For each packet:
//	  Packet header (16 bytes)
//	  Packet data
//
// We write two capture types:
//   - NGAP over SCTP over IP (link type: LINUX_SLL / DLT 113)
//   - GTP-U over UDP over IP (link type: EN10MB / DLT 1)
//
// Ref: https://wiki.wireshark.org/Development/LibpcapFileFormat
// Ref: https://wiki.wireshark.org/5G_NR
package pcap

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// LinkType identifies the data link layer type in a pcap file.
// Wireshark uses this to select the right dissector chain.
const (
	// LinkTypeEthernet is standard Ethernet framing (DLT_EN10MB).
	// Used for UDP/GTP-U captures.
	LinkTypeEthernet uint32 = 1

	// LinkTypeLinuxSLL is Linux "cooked" capture (DLT_LINUX_SLL).
	// Used for SCTP/NGAP — avoids needing a real Ethernet header.
	LinkTypeLinuxSLL uint32 = 113
)

// pcapMagic is the libpcap file magic number (little-endian).
const pcapMagic uint32 = 0xa1b2c3d4

// pcapVersionMajor / pcapVersionMinor are the libpcap format version.
const (
	pcapVersionMajor uint16 = 2
	pcapVersionMinor uint16 = 4
)

// Writer writes packets to a libpcap file.
// Safe for concurrent use — multiple goroutines can write simultaneously.
type Writer struct {
	mu       sync.Mutex
	f        *os.File
	linkType uint32
	count    int
}

// NewWriter creates a new pcap file at the given path.
// linkType determines which Wireshark dissector chain is used.
func NewWriter(path string, linkType uint32) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create pcap file %s: %w", path, err)
	}

	w := &Writer{f: f, linkType: linkType}
	if err := w.writeGlobalHeader(); err != nil {
		f.Close()
		return nil, fmt.Errorf("write pcap header: %w", err)
	}

	fmt.Printf("[PCAP] Writing to %s (link type %d)\n", path, linkType)
	return w, nil
}

// writeGlobalHeader writes the 24-byte pcap global header.
// Ref: https://wiki.wireshark.org/Development/LibpcapFileFormat §Global Header
func (w *Writer) writeGlobalHeader() error {
	hdr := make([]byte, 24)
	binary.LittleEndian.PutUint32(hdr[0:], pcapMagic)
	binary.LittleEndian.PutUint16(hdr[4:], pcapVersionMajor)
	binary.LittleEndian.PutUint16(hdr[6:], pcapVersionMinor)
	binary.LittleEndian.PutUint32(hdr[8:], 0)      // thiszone: UTC
	binary.LittleEndian.PutUint32(hdr[12:], 0)     // sigfigs: 0
	binary.LittleEndian.PutUint32(hdr[16:], 65535) // snaplen: max
	binary.LittleEndian.PutUint32(hdr[20:], w.linkType)
	_, err := w.f.Write(hdr)
	return err
}

// WritePacket appends a raw packet to the pcap file with a timestamp.
// data should be the full frame starting from the link layer.
func (w *Writer) WritePacket(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	sec := uint32(now.Unix())
	usec := uint32(now.Nanosecond() / 1000)
	capLen := uint32(len(data))
	origLen := uint32(len(data))

	// 16-byte packet record header
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], sec)
	binary.LittleEndian.PutUint32(hdr[4:], usec)
	binary.LittleEndian.PutUint32(hdr[8:], capLen)
	binary.LittleEndian.PutUint32(hdr[12:], origLen)

	if _, err := w.f.Write(hdr); err != nil {
		return err
	}
	if _, err := w.f.Write(data); err != nil {
		return err
	}

	w.count++
	return nil
}

// Close flushes and closes the pcap file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Printf("[PCAP] Closed (%d packets written)\n", w.count)
	return w.f.Close()
}

// Count returns the number of packets written so far.
func (w *Writer) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.count
}

// --- Frame builders ---
// These wrap raw protocol payloads in the appropriate link/IP/transport
// headers so Wireshark can apply its full dissector chain.

// BuildSCTPFrame wraps a raw NGAP payload in Linux SLL + IP + SCTP headers.
// Wireshark will decode this as SCTP carrying NGAP automatically.
//
// Linux SLL (cooked capture) avoids needing a real Ethernet MAC address.
// Ref: https://wiki.wireshark.org/SLL
func BuildSCTPFrame(srcIP, dstIP net.IP, srcPort, dstPort uint16, ngapPayload []byte) []byte {
	// Linux SLL header (16 bytes)
	// Ref: https://www.tcpdump.org/linktypes/LINKTYPE_LINUX_SLL.html
	sll := make([]byte, 16)
	binary.BigEndian.PutUint16(sll[0:], 4) // packet type: sent by us
	binary.BigEndian.PutUint16(sll[2:], 0) // ARPHRD_VOID
	binary.BigEndian.PutUint16(sll[4:], 0) // link address length
	// bytes 6-13: link address (zero for loopback)
	binary.BigEndian.PutUint16(sll[14:], 0x0800) // protocol: IPv4

	// SCTP header (12 bytes minimum)
	// Ref: RFC 4960 §3.1
	sctp := make([]byte, 12)
	binary.BigEndian.PutUint16(sctp[0:], srcPort)
	binary.BigEndian.PutUint16(sctp[2:], dstPort)
	binary.BigEndian.PutUint32(sctp[4:], 1) // Verification Tag
	binary.BigEndian.PutUint32(sctp[8:], 0) // Checksum (0 = not computed)

	// SCTP DATA chunk (16 bytes header + payload)
	// Ref: RFC 4960 §3.3.1
	chunkLen := uint16(16 + len(ngapPayload))
	chunk := make([]byte, 16)
	chunk[0] = 0x00 // chunk type: DATA
	chunk[1] = 0x03 // flags: beginning + ending fragment
	binary.BigEndian.PutUint16(chunk[2:], chunkLen)
	binary.BigEndian.PutUint32(chunk[4:], 1)   // TSN
	binary.BigEndian.PutUint16(chunk[8:], 1)   // Stream ID
	binary.BigEndian.PutUint16(chunk[10:], 0)  // Stream Seq Number
	binary.BigEndian.PutUint32(chunk[12:], 60) // Payload Protocol ID: NGAP = 60

	// IPv4 header
	sctpPayload := append(chunk, ngapPayload...)
	ip := buildIPv4Header(srcIP, dstIP, 132, len(sctpPayload)+len(sctp)) // 132 = SCTP
	sctpFrame := append(sctp, sctpPayload...)

	return concat(sll, ip, sctpFrame)
}

// BuildUDPFrame wraps a GTP-U payload in Ethernet + IP + UDP headers.
// Wireshark recognises UDP port 2152 as GTP-U automatically.
func BuildUDPFrame(srcIP, dstIP net.IP, srcPort, dstPort uint16, gtpPayload []byte) []byte {
	// Ethernet header (14 bytes) — fake MACs for loopback
	eth := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // dst MAC
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // src MAC
		0x08, 0x00, // EtherType: IPv4
	}

	// UDP header (8 bytes)
	// Ref: RFC 768
	udpLen := uint16(8 + len(gtpPayload))
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:], srcPort)
	binary.BigEndian.PutUint16(udp[2:], dstPort)
	binary.BigEndian.PutUint16(udp[4:], udpLen)
	binary.BigEndian.PutUint16(udp[6:], 0) // checksum: 0 = not computed

	ip := buildIPv4Header(srcIP, dstIP, 17, int(udpLen)) // 17 = UDP
	udpFrame := append(udp, gtpPayload...)

	return concat(eth, ip, udpFrame)
}

// buildIPv4Header builds a minimal IPv4 header.
// Ref: RFC 791
func buildIPv4Header(src, dst net.IP, proto uint8, payloadLen int) []byte {
	totalLen := uint16(20 + payloadLen)
	ip := make([]byte, 20)
	ip[0] = 0x45                                 // version=4, IHL=5
	ip[1] = 0x00                                 // DSCP/ECN
	binary.BigEndian.PutUint16(ip[2:], totalLen) // total length
	binary.BigEndian.PutUint16(ip[4:], 0x0001)   // ID
	binary.BigEndian.PutUint16(ip[6:], 0x4000)   // DF flag, no fragment
	ip[8] = 64                                   // TTL
	ip[9] = proto                                // protocol
	// checksum: left 0 — Wireshark will compute it
	copy(ip[12:16], src.To4())
	copy(ip[16:20], dst.To4())
	return ip
}

// concat joins byte slices efficiently.
func concat(slices ...[]byte) []byte {
	total := 0
	for _, s := range slices {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
