package upf

import (
	"net"

	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

func summarizeIPv4(pkt []byte) (src, dst string, proto uint8, ok bool) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return "", "", 0, false
	}
	src = net.IP(pkt[12:16]).String()
	dst = net.IP(pkt[16:20]).String()
	return src, dst, pkt[9], true
}

func icmpTypeName(pkt []byte) string {
	if len(pkt) < 21 || pkt[9] != 1 {
		return ""
	}
	switch pkt[20] {
	case 8:
		return "ICMP echo request"
	case 0:
		return "ICMP echo reply"
	default:
		return "ICMP"
	}
}

func emitUPFUplinkObs(sess *UPFSession, pkt []byte) {
	src, dst, proto, ok := summarizeIPv4(pkt)
	if !ok || !obspub.Enabled() {
		return
	}
	detail := protoName(proto)
	if n := icmpTypeName(pkt); n != "" {
		detail = n
	}
	obspub.EmitPacket(seqdiag.NodeUPF, seqdiag.NodeGNB, "ul",
		src+" → "+dst+" ("+detail+") "+byteSize(len(pkt)),
		"TS 29.281 §5.1",
		map[string]string{
			"ue_ip": sess.UEIPAddress,
			"teid":  teidHex(sess.TEID),
		})
}

func emitUPFInjectObs(pkt []byte) {
	src, dst, proto, ok := summarizeIPv4(pkt)
	if !ok || !obspub.Enabled() {
		return
	}
	detail := protoName(proto)
	if n := icmpTypeName(pkt); n != "" {
		detail = n
	}
	obspub.EmitPacket(seqdiag.NodeUPF, seqdiag.NodeUE, "n6_inject",
		src+" → "+dst+" ("+detail+") "+byteSize(len(pkt)),
		"TS 23.501 §5.8.2.11.3",
		nil)
}

func emitUPFDownlinkObs(sess *UPFSession, pkt []byte) {
	src, dst, proto, ok := summarizeIPv4(pkt)
	if !ok || !obspub.Enabled() {
		return
	}
	detail := protoName(proto)
	if n := icmpTypeName(pkt); n != "" {
		detail = n
	}
	obspub.EmitPacket(seqdiag.NodeUPF, seqdiag.NodeGNB, "dl",
		src+" → "+dst+" ("+detail+") "+byteSize(len(pkt)),
		"TS 29.281 §5.1",
		map[string]string{
			"ue_ip": sess.UEIPAddress,
			"teid":  teidHex(sess.GNTEID),
		})
}

func protoName(p uint8) string {
	switch p {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	default:
		return "IPv4"
	}
}

func byteSize(n int) string {
	if n == 1 {
		return "1 byte"
	}
	return itoaUint(n) + " bytes"
}

func itoaUint(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func teidHex(t uint32) string {
	return "0x" + hex8(t)
}

func hex8(t uint32) string {
	const hexd = "0123456789ABCDEF"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = hexd[t&0xF]
		t >>= 4
	}
	return string(b[:])
}
