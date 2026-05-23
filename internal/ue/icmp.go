package ue

import (
	"encoding/binary"
	"errors"
	"net"
)

// buildICMPEchoRequest builds an IPv4 ICMP echo request from src to dst.
// Ref: RFC 792
func buildICMPEchoRequest(srcIP, dstIP string, identifier, seq uint16) ([]byte, error) {
	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()
	if src == nil || dst == nil {
		return nil, errors.New("invalid IPv4 address")
	}
	const icmpLen = 8
	totalLen := 20 + icmpLen
	pkt := make([]byte, totalLen)

	pkt[0] = 0x45 // version + IHL
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64  // TTL
	pkt[9] = 1   // ICMP
	copy(pkt[12:16], src)
	copy(pkt[16:20], dst)

	off := 20
	pkt[off] = 8 // Echo Request
	pkt[off+1] = 0
	binary.BigEndian.PutUint16(pkt[off+4:off+6], identifier)
	binary.BigEndian.PutUint16(pkt[off+6:off+8], seq)

	cs := internetChecksum(pkt[off:])
	pkt[off+2] = byte(cs >> 8)
	pkt[off+3] = byte(cs)

	ipCS := internetChecksum(pkt[:20])
	pkt[10] = byte(ipCS >> 8)
	pkt[11] = byte(ipCS)
	return pkt, nil
}

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

func isICMPEchoReply(pkt []byte, expectedDst net.IP) bool {
	if len(pkt) < 28 || pkt[0]>>4 != 4 || pkt[9] != 1 {
		return false
	}
	if expectedDst != nil && !net.IP(pkt[16:20]).Equal(expectedDst.To4()) {
		return false
	}
	return pkt[20] == 0 // Echo Reply
}
