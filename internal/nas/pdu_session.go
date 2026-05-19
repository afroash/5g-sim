// pdu_session.go — NAS 5GS Session Management messages.
//
// These are the NAS messages exchanged between UE and SMF for
// PDU session management. They travel inside NAS MM messages
// (wrapped in NGAP) between the UE and AMF, and the AMF
// forwards them to/from the SMF.
//
// Ref: TS 24.501 §8.3 — 5GS session management messages
package nas

import (
	"encoding/binary"
	"fmt"
	"net"
)

// SM message types (5GS Session Management).
// These are carried inside a NAS MM message as a container.
// Ref: TS 24.501 §9.7 Table 9.7.1
const (
	MsgTypePDUSessionEstablishmentRequest = 0xC1
	MsgTypePDUSessionEstablishmentAccept  = 0xC2
	MsgTypePDUSessionEstablishmentReject  = 0xC3
	MsgTypePDUSessionReleaseRequest       = 0xD1
	MsgTypePDUSessionReleaseReject        = 0xD2
	MsgTypePDUSessionReleaseCommand       = 0xD3
	MsgTypePDUSessionReleaseComplete      = 0xD4
)

// PDU Session Establishment Request — sent by UE to request a data session.
// Ref: TS 24.501 §8.3.1
type PDUSessionEstablishmentRequest struct {
	PDUSessionID   uint8
	PDUSessionType uint8  // 1=IPv4, 2=IPv6, 3=IPv4v6, 4=Unstructured
	SSCMode        uint8  // Session and Service Continuity Mode
	RequestedDNN   string // Data Network Name e.g. "internet"
	SNssai         *SNSSAI
}

// PDUSessionEstablishmentAccept — sent by network to confirm session.
// Ref: TS 24.501 §8.3.2
type PDUSessionEstablishmentAccept struct {
	PDUSessionID       uint8
	PDUSessionType     uint8
	AuthorizedQosRules []byte // simplified
	SessionAMBR        []byte // Aggregate Maximum Bit Rate
	AllocatedIP        string // The IP address given to the UE
	SNssai             *SNSSAI
	DNN                string
	// DownlinkTEID is a simulator extension (gNB DL F-TEID for GTP-U toward UE).
	DownlinkTEID uint32
}

// IEIUserPlaneDLTEID is a private IE carrying the gNB downlink GTP-U TEID.
const IEIUserPlaneDLTEID = 0x78

// PDU Session type values.
// Ref: TS 24.501 §9.11.4.11
const (
	PDUSessTypeIPv4         = 0x01
	PDUSessTypeIPv6         = 0x02
	PDUSessTypeIPv4v6       = 0x03
	PDUSessTypeUnstructured = 0x04
)

// SSC Mode values — Session and Service Continuity.
// Ref: TS 24.501 §9.11.4.16
const (
	SSCMode1 = 0x01 // Anchor UPF maintained throughout session
	SSCMode2 = 0x02 // New anchor may be selected on mobility
	SSCMode3 = 0x03 // Old session maintained while new is set up
)

// BuildPDUSessionEstablishmentRequest encodes a PDU Session Establishment Request.
//
// This is a 5GS SM message. In the full protocol it is carried inside
// a NAS MM UL NAS Transport message as a container.
//
// Byte layout (TS 24.501 §8.3.1):
//
//	[0]   EPD = 0x2E (5GS SM)
//	[1]   PDU Session ID
//	[2]   PTI (Procedure Transaction Identity)
//	[3]   Message Type = 0xC1
//	[4]   PDU Session Type + SSC Mode
//	[5..] Optional IEs (DNN, NSSAI...)
func BuildPDUSessionEstablishmentRequest(pduSessionID uint8, dnn string) []byte {
	msg := []byte{
		EPD5GSSessionManagement,               // byte 0: 5GS SM
		pduSessionID,                          // byte 1: PDU Session ID
		0x01,                                  // byte 2: PTI = 1
		MsgTypePDUSessionEstablishmentRequest, // byte 3: message type
		(PDUSessTypeIPv4 & 0x0F) | ((SSCMode1 & 0x07) << 4), // byte 4: type + SSC
	}

	// Optional IE: DNN (Data Network Name)
	// Ref: TS 24.501 §9.11.2.1A
	if dnn != "" {
		// DNN is length-prefixed, then the DNN string as a DNS-style label
		// Ref: TS 24.501 §9.11.2.1A
		dnnBytes := encodeDNN(dnn)
		msg = append(msg, 0x25) // IEI for DNN
		msg = append(msg, byte(len(dnnBytes)))
		msg = append(msg, dnnBytes...)
	}

	// Optional IE: Requested NSSAI — ask for eMBB slice
	// Ref: TS 24.501 §9.11.3.37
	msg = append(msg,
		0x22, // IEI for Requested NSSAI (in SM context)
		0x02, // length
		0x01, // SNSSAI length
		0x01, // SST = 1 (eMBB)
	)

	return msg
}

// BuildPDUSessionEstablishmentAccept encodes a PDU Session Establishment Accept.
//
// Sent by the network (via AMF) to confirm the session and provide
// the allocated IP address and QoS parameters.
//
// Ref: TS 24.501 §8.3.2
func BuildPDUSessionEstablishmentAccept(pduSessionID uint8, allocatedIP string, dnn string) []byte {
	msg := []byte{
		EPD5GSSessionManagement,              // byte 0
		pduSessionID,                         // byte 1: PDU Session ID
		0x01,                                 // byte 2: PTI
		MsgTypePDUSessionEstablishmentAccept, // byte 3
		PDUSessTypeIPv4,                      // byte 4: PDU session type
	}

	// Authorized QoS Rules — mandatory IE (simplified: one default bearer rule)
	// Ref: TS 24.501 §9.11.4.13
	qosRules := buildDefaultQoSRules()
	msg = append(msg, byte(len(qosRules)>>8), byte(len(qosRules)))
	msg = append(msg, qosRules...)

	// Session AMBR — mandatory: max bit rate for the session
	// Ref: TS 24.501 §9.11.4.14 — 6 content bytes: DL(unit 1B + value 2B) + UL(unit 1B + value 2B)
	// 100 Mbps in each direction (unit=6 means 1Mbps, value=100 → 100Mbps)
	sessionAMBR := []byte{
		0x06,             // length = 6 content bytes
		0x06, 0x00, 0x64, // DL: unit=6 (1Mbps), value=100 (2 bytes BE) → 100Mbps
		0x06, 0x00, 0x64, // UL: unit=6 (1Mbps), value=100 (2 bytes BE) → 100Mbps
	}
	msg = append(msg, sessionAMBR...)

	// Optional IE: PDU address (the allocated IP)
	// Ref: TS 24.501 §9.11.4.10
	if allocatedIP != "" {
		ipBytes := encodeIPv4(allocatedIP)
		if ipBytes != nil {
			msg = append(msg, 0x29)                 // IEI for PDU address
			msg = append(msg, byte(1+len(ipBytes))) // length
			msg = append(msg, PDUSessTypeIPv4)      // PDU session type
			msg = append(msg, ipBytes...)
		}
	}

	// Optional IE: DNN
	if dnn != "" {
		dnnBytes := encodeDNN(dnn)
		msg = append(msg, 0x25)
		msg = append(msg, byte(len(dnnBytes)))
		msg = append(msg, dnnBytes...)
	}

	return msg
}

// AppendDownlinkTEID adds the simulator user-plane DL TEID IE to an encoded accept PDU.
func AppendDownlinkTEID(msg []byte, dlTEID uint32) []byte {
	if dlTEID == 0 {
		return msg
	}
	teid := make([]byte, 4)
	binary.BigEndian.PutUint32(teid, dlTEID)
	out := append(msg, IEIUserPlaneDLTEID, byte(len(teid)))
	return append(out, teid...)
}

// BuildPDUSessionEstablishmentReject encodes a PDU Session Establishment Reject.
// Ref: TS 24.501 §8.3.3
func BuildPDUSessionEstablishmentReject(pduSessionID uint8, cause uint8) []byte {
	return []byte{
		EPD5GSSessionManagement,
		pduSessionID,
		0x01, // PTI
		MsgTypePDUSessionEstablishmentReject,
		cause,
	}
}

// DecodePDUSessionEstablishmentRequest parses a PDU Session Establishment Request.
// Ref: TS 24.501 §8.3.1
func DecodePDUSessionEstablishmentRequest(data []byte) (*PDUSessionEstablishmentRequest, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("PDU session request too short: %d bytes", len(data))
	}

	req := &PDUSessionEstablishmentRequest{
		PDUSessionID:   data[1],
		PDUSessionType: data[4] & 0x0F,
		SSCMode:        (data[4] >> 4) & 0x07,
	}

	// Parse optional IEs starting at offset 5
	offset := 5
	for offset < len(data)-1 {
		iei := data[offset]
		offset++
		if offset >= len(data) {
			break
		}
		ieLen := int(data[offset])
		offset++
		if offset+ieLen > len(data) {
			break
		}
		ieData := data[offset : offset+ieLen]
		offset += ieLen

		switch iei {
		case 0x25: // DNN
			req.RequestedDNN = decodeDNN(ieData)
		}
	}

	return req, nil
}

// --- helpers ---

// buildDefaultQoSRules builds a minimal default QoS rule for a PDU session.
// Ref: TS 24.501 §9.11.4.13
func buildDefaultQoSRules() []byte {
	// One rule: QoS Rule ID=1, DQR=1 (default), QFI=1
	return []byte{
		0x01,       // QoS Rule ID = 1
		0x00, 0x06, // Length = 6
		0x31, // Rule operation code=001 (create), DQR=1, number of packet filters=1
		0x01, // Packet filter direction=0x01 (bidirectional) + ID=1
		0x01, // Packet filter length = 1
		0x01, // Match-all filter
		0x01, // Precedence = 1
		0x01, // QFI = 1
	}
}

// encodeDNN encodes a DNN string as a DNS-style label sequence.
// e.g. "internet" → [8, 'i','n','t','e','r','n','e','t']
// Ref: TS 24.501 §9.11.2.1A
func encodeDNN(dnn string) []byte {
	// Simple encoding: length byte + ASCII bytes
	result := []byte{byte(len(dnn))}
	result = append(result, []byte(dnn)...)
	return result
}

// decodeDNN reverses encodeDNN.
func decodeDNN(data []byte) string {
	if len(data) < 1 {
		return ""
	}
	l := int(data[0])
	if l+1 > len(data) {
		return ""
	}
	return string(data[1 : 1+l])
}

// encodeIPv4 encodes an IPv4 address string to 4 bytes.
func encodeIPv4(ip string) []byte {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	return parsed.To4()
}
