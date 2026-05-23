package ue

import (
	"fmt"
	"net"

	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

func emitUEUplinkObs(ueIP, supi string, teid uint32, pkt []byte) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 || !obspub.Enabled() {
		return
	}
	src := net.IP(pkt[12:16]).String()
	dst := net.IP(pkt[16:20]).String()
	detail := ipv4ProtoName(pkt[9])
	if pkt[9] == 1 && len(pkt) >= 21 {
		if pkt[20] == 8 {
			detail = "ICMP echo request"
		} else if pkt[20] == 0 {
			detail = "ICMP echo reply"
		}
	}
	obspub.EmitPacket(seqdiag.NodeUE, seqdiag.NodeGNB, "ul",
		src+" → "+dst+" ("+detail+") "+fmt.Sprintf("%d bytes", len(pkt)),
		"TS 29.281 §5.1",
		map[string]string{"supi": supi, "teid": fmt.Sprintf("0x%08X", teid)})
}

func emitUEDownlinkObs(supi string, teid uint32, pkt []byte) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 || !obspub.Enabled() {
		return
	}
	src := net.IP(pkt[12:16]).String()
	dst := net.IP(pkt[16:20]).String()
	detail := ipv4ProtoName(pkt[9])
	if pkt[9] == 1 && len(pkt) >= 21 {
		if pkt[20] == 8 {
			detail = "ICMP echo request"
		} else if pkt[20] == 0 {
			detail = "ICMP echo reply"
		}
	}
	obspub.EmitPacket(seqdiag.NodeGNB, seqdiag.NodeUE, "dl",
		src+" → "+dst+" ("+detail+") "+fmt.Sprintf("%d bytes", len(pkt)),
		"TS 29.281 §5.1",
		map[string]string{"supi": supi, "teid": fmt.Sprintf("0x%08X", teid)})
}

func ipv4ProtoName(p uint8) string {
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
