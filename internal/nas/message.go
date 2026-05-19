// message.go — NAS message encode/decode for the Registration procedure.
//
// NAS messages are encoded as simple byte arrays — no APER/ASN.1 here.
// The format is defined field-by-field in TS 24.501 §8.2.x.
//
// We implement a minimal codec that covers exactly what's needed for
// the Registration procedure. Each function is annotated with the
// exact spec section and field offset.
//
// Ref: TS 24.501 §8.2 — NAS message formats
package nas

import (
	"encoding/binary"
	"fmt"
)

// Message is a decoded NAS message header.
// All NAS messages start with these three fields.
//
// Ref: TS 24.501 §9.1.1
type Message struct {
	EPD            uint8  // Extended Protocol Discriminator
	SecurityHeader uint8  // Security Header Type
	MessageType    uint8  // What kind of message this is
	Payload        []byte // Raw bytes of IEs following the header
}

// RegistrationRequest is a decoded 5GS Registration Request.
// Sent by the UE to initiate registration.
//
// Ref: TS 24.501 §8.2.6
type RegistrationRequest struct {
	// Mandatory IEs
	RegistrationType uint8  // Initial/Mobility/Periodic (lower 3 bits)
	FollowOnRequest  bool   // UE has pending data after registration
	NASKeySetID      uint8  // Key set identifier
	MobileIdentity   []byte // SUCI or 5G-GUTI raw bytes

	// Optional IEs (present depends on registration type)
	UESecCapability *UESecurityCapability
	RequestedNSSAI  []SNSSAI
	UECapabilities  []byte // raw — extended for future use
}

// RegistrationAccept is sent by the AMF to accept a registration.
//
// Ref: TS 24.501 §8.2.7
type RegistrationAccept struct {
	// Mandatory IEs
	RegistrationResult uint8 // 3GPP / Non-3GPP / Both

	// Optional IEs
	GUTI5G       *GUTI5G  // New GUTI assigned by AMF
	AllowedNSSAI []SNSSAI // Slices the UE is allowed to use
	TAIList      []byte   // Tracking areas the registration covers
	T3512        uint8    // Periodic registration timer value
}

// RegistrationReject is sent by the AMF when it rejects registration.
//
// Ref: TS 24.501 §8.2.8
type RegistrationReject struct {
	Cause uint8 // Why registration was rejected
}

// --- Encoder ---

// BuildRegistrationRequest encodes a NAS Registration Request.
//
// Byte layout (TS 24.501 §8.2.6):
//
//	[0]   EPD = 0x7E (5GS MM)
//	[1]   Security Header = 0x00 (plain)
//	[2]   Message Type = 0x41
//	[3]   Registration Type + Follow-on Request bit
//	[4]   NAS Key Set Identifier
//	[5..] Mobile Identity IE
func BuildRegistrationRequest(supi SUPI, regType uint8, followOn bool) []byte {
	msg := []byte{
		EPD5GSMobilityManagement,   // byte 0: EPD
		SecurityHeaderTypePlain,    // byte 1: no security yet
		MsgTypeRegistrationRequest, // byte 2: message type
	}

	// byte 3: registration type (3 bits) | follow-on request (1 bit)
	// Ref: TS 24.501 §9.11.3.7
	regTypeByte := regType & 0x07
	if followOn {
		regTypeByte |= FollowOnRequestPending
	}
	msg = append(msg, regTypeByte)

	// byte 4: NAS Key Set Identifier — 0x0E = "no key available" for initial reg
	// Ref: TS 24.501 §9.11.3.32
	msg = append(msg, 0x0E)

	// bytes 5+: Mobile Identity — encode SUPI as IMSI-type identity
	// Ref: TS 24.501 §9.11.3.4 / TS 24.008 §10.5.1.4
	imsiBytes := encodeSUPI(supi)
	// Length prefix for the IE
	msg = append(msg, byte(len(imsiBytes)))
	msg = append(msg, imsiBytes...)

	// Optional: UE Security Capability IE
	// Ref: TS 24.501 §9.11.3.54
	// We advertise: NEA0 (null), NEA2 (AES), NIA0 (null), NIA2 (SNOW3G)
	msg = append(msg,
		0x2E, // IEI for UE Security Capability
		0x02, // length: 2 bytes
		0xC0, // 5G-EA: NEA0=1, NEA2=1, rest=0
		0xC0, // 5G-IA: NIA0=1, NIA2=1, rest=0
	)

	// Optional: Requested NSSAI — ask for eMBB slice (SST=1)
	// Ref: TS 24.501 §9.11.3.37
	msg = append(msg,
		0x2F, // IEI for Requested NSSAI
		0x02, // length: 2 bytes (one SNSSAI with SST only)
		0x01, // length of this SNSSAI entry
		0x01, // SST = 1 (eMBB)
	)

	return msg
}

// BuildRegistrationAccept encodes a NAS Registration Accept.
//
// Byte layout (TS 24.501 §8.2.7):
//
//	[0]   EPD
//	[1]   Security Header
//	[2]   Message Type = 0x42
//	[3]   5GS Registration Result
//	[4..] Optional IEs (GUTI, Allowed NSSAI, TAI list, timers...)
func BuildRegistrationAccept(result uint8, guti *GUTI5G, allowedNSSAI []SNSSAI) []byte {
	msg := []byte{
		EPD5GSMobilityManagement,  // byte 0
		SecurityHeaderTypePlain,   // byte 1
		MsgTypeRegistrationAccept, // byte 2
		result,                    // byte 3: registration result
	}

	// Optional IE: 5G-GUTI
	// Ref: TS 24.501 §9.11.3.4
	if guti != nil {
		gutiBytes := encodeGUTI(guti)
		msg = append(msg, IEI5GSGUTI)
		msg = append(msg, byte(len(gutiBytes)))
		msg = append(msg, gutiBytes...)
	}

	// Optional IE: Allowed NSSAI
	// Ref: TS 24.501 §9.11.3.37
	if len(allowedNSSAI) > 0 {
		nssaiBytes := encodeNSSAI(allowedNSSAI)
		msg = append(msg, byte(IEIAllowedNSSAI))
		msg = append(msg, byte(len(nssaiBytes)))
		msg = append(msg, nssaiBytes...)
	}

	// Optional IE: T3512 — periodic registration timer (54 minutes)
	// Ref: TS 24.501 §9.11.3.18 / GPRS timer 3
	msg = append(msg,
		byte(IEIT3512Value),
		0x01, // length
		0x2D, // value: 54 minutes (binary: 0b00101101 = unit*4 minutes, val=13 → 52min)
	)

	return msg
}

// BuildRegistrationComplete encodes the NAS Registration Complete.
// Sent by UE to confirm it has stored the new GUTI.
//
// Ref: TS 24.501 §8.2.9
func BuildRegistrationComplete() []byte {
	return []byte{
		EPD5GSMobilityManagement,    // byte 0
		SecurityHeaderTypePlain,     // byte 1
		MsgTypeRegistrationComplete, // byte 2
		// No mandatory IEs beyond the header
	}
}

// BuildRegistrationReject encodes a NAS Registration Reject.
//
// Ref: TS 24.501 §8.2.8
func BuildRegistrationReject(cause uint8) []byte {
	return []byte{
		EPD5GSMobilityManagement,  // byte 0
		SecurityHeaderTypePlain,   // byte 1
		MsgTypeRegistrationReject, // byte 2
		cause,                     // byte 3: 5GMM cause
	}
}

// --- Decoder ---

// Decode parses the header of any NAS message.
// Returns the message type and raw payload bytes for further parsing.
//
// Ref: TS 24.501 §9.1.1
func Decode(data []byte) (*Message, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("NAS message too short: %d bytes", len(data))
	}

	msg := &Message{
		EPD:            data[0],
		SecurityHeader: data[1],
		MessageType:    data[2],
	}

	if len(data) > 3 {
		msg.Payload = data[3:]
	}

	return msg, nil
}

// DecodeRegistrationRequest parses a Registration Request payload.
// Call after Decode() confirms MessageType == MsgTypeRegistrationRequest.
//
// Ref: TS 24.501 §8.2.6
func DecodeRegistrationRequest(payload []byte) (*RegistrationRequest, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("registration request payload too short")
	}

	req := &RegistrationRequest{}

	// byte 0: registration type (bits 2-0) + follow-on (bit 3)
	req.RegistrationType = payload[0] & 0x07
	req.FollowOnRequest = (payload[0] & 0x08) != 0

	// byte 1: NAS Key Set Identifier
	req.NASKeySetID = payload[1]

	// bytes 2+: Mobile Identity IE (length-prefixed)
	offset := 2
	if offset < len(payload) {
		idLen := int(payload[offset])
		offset++
		if offset+idLen <= len(payload) {
			req.MobileIdentity = payload[offset : offset+idLen]
			offset += idLen
		}
	}

	// Parse optional IEs
	for offset < len(payload)-1 {
		iei := payload[offset]
		offset++
		if offset >= len(payload) {
			break
		}
		ieLen := int(payload[offset])
		offset++
		if offset+ieLen > len(payload) {
			break
		}
		ieData := payload[offset : offset+ieLen]
		offset += ieLen

		switch iei {
		case 0x2E: // UE Security Capability
			if len(ieData) >= 2 {
				req.UESecCapability = &UESecurityCapability{
					NEA0: ieData[0]&0x80 != 0,
					NEA2: ieData[0]&0x20 != 0,
					NIA0: len(ieData) > 1 && ieData[1]&0x80 != 0,
					NIA2: len(ieData) > 1 && ieData[1]&0x20 != 0,
				}
			}
		case 0x2F: // Requested NSSAI
			req.RequestedNSSAI = decodeNSSAI(ieData)
		}
	}

	return req, nil
}

// DecodeRegistrationAccept parses a Registration Accept payload.
// Ref: TS 24.501 §8.2.7
func DecodeRegistrationAccept(payload []byte) (*RegistrationAccept, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("registration accept payload too short")
	}

	acc := &RegistrationAccept{
		RegistrationResult: payload[0] & 0x07,
	}

	offset := 1
	for offset < len(payload)-1 {
		iei := payload[offset]
		offset++
		if offset >= len(payload) {
			break
		}
		ieLen := int(payload[offset])
		offset++
		if offset+ieLen > len(payload) {
			break
		}
		ieData := payload[offset : offset+ieLen]
		offset += ieLen

		switch iei {
		case IEI5GSGUTI:
			acc.GUTI5G = decodeGUTI(ieData)
		case byte(IEIAllowedNSSAI):
			acc.AllowedNSSAI = decodeNSSAI(ieData)
		case byte(IEIT3512Value):
			if len(ieData) >= 1 {
				acc.T3512 = ieData[0]
			}
		}
	}

	return acc, nil
}

// BuildULNASTransportMM wraps an SM payload in a NAS MM UL NAS Transport message.
// Used by the UE to send SM messages (e.g. PDU Session Request) toward the network.
// Ref: TS 24.501 §8.2.14
func BuildULNASTransportMM(pduSessionID uint8, smPayload []byte) []byte {
	msg := []byte{
		EPD5GSMobilityManagement,
		SecurityHeaderTypePlain,
		0x67, // UL NAS Transport message type
		0x01, // Payload container type = N1 SM info
	}
	msg = append(msg, byte(len(smPayload)>>8), byte(len(smPayload)))
	msg = append(msg, smPayload...)
	msg = append(msg, 0x12, pduSessionID) // PDU Session ID IE
	return msg
}

// BuildDLNASTransportMM wraps an SM payload in a NAS MM DL NAS Transport message.
// Used by the network to deliver SM messages to the UE.
// Ref: TS 24.501 §8.2.15
func BuildDLNASTransportMM(pduSessionID uint8, smPayload []byte) []byte {
	msg := []byte{
		EPD5GSMobilityManagement,
		SecurityHeaderTypePlain,
		0x68, // DL NAS Transport message type
		0x01, // Payload container type = N1 SM info
	}
	msg = append(msg, byte(len(smPayload)>>8), byte(len(smPayload)))
	msg = append(msg, smPayload...)
	msg = append(msg, 0x12, pduSessionID)
	return msg
}

// DecodeDLNASTransport parses a NAS MM DL NAS Transport (type 0x68) message.
// Returns the PDU session ID and the SM container payload.
// Ref: TS 24.501 §8.2.15
func DecodeDLNASTransport(data []byte) (pduSessionID uint8, smPayload []byte, err error) {
	// Header: EPD(1) + SecHdr(1) + MsgType(1) + ContainerType(1) + ContainerLen(2) = 6 bytes
	if len(data) < 6 {
		return 0, nil, fmt.Errorf("nas: DL NAS Transport too short: %d bytes", len(data))
	}
	if data[2] != 0x68 {
		return 0, nil, fmt.Errorf("nas: expected DL NAS Transport (0x68), got 0x%02X", data[2])
	}
	containerLen := int(data[4])<<8 | int(data[5])
	if 6+containerLen > len(data) {
		return 0, nil, fmt.Errorf("nas: DL NAS Transport container length exceeds message")
	}
	smPayload = data[6 : 6+containerLen]
	// PDU Session ID IE (IEI=0x12) follows the container
	offset := 6 + containerLen
	for offset+1 < len(data) {
		if data[offset] == 0x12 {
			pduSessionID = data[offset+1]
			break
		}
		offset++
	}
	return pduSessionID, smPayload, nil
}

// DecodePDUSessionEstablishmentAccept parses a 5GS SM PDU Session Establishment Accept.
// Extracts the allocated UE IP address and DNN.
// Ref: TS 24.501 §8.3.2
func DecodePDUSessionEstablishmentAccept(data []byte) (*PDUSessionEstablishmentAccept, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("nas: PDU Session Accept too short: %d bytes", len(data))
	}
	acc := &PDUSessionEstablishmentAccept{
		PDUSessionID: data[1],
	}
	// Skip mandatory QoS Rules (2-byte length prefix)
	offset := 5
	if offset+2 > len(data) {
		return acc, nil
	}
	qosLen := int(data[offset])<<8 | int(data[offset+1])
	offset += 2 + qosLen
	// Skip Session AMBR (1-byte length prefix)
	if offset+1 > len(data) {
		return acc, nil
	}
	ambrLen := int(data[offset])
	offset += 1 + ambrLen
	// Parse optional IEs
	for offset+1 < len(data) {
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
		case 0x29: // PDU Address
			if len(ieData) >= 5 && ieData[0] == 0x01 {
				acc.AllocatedIP = fmt.Sprintf("%d.%d.%d.%d",
					ieData[1], ieData[2], ieData[3], ieData[4])
			}
		case 0x25: // DNN
			acc.DNN = decodeDNN(ieData)
		case IEIUserPlaneDLTEID: // simulator DL GTP-U TEID
			if len(ieData) >= 4 {
				acc.DownlinkTEID = binary.BigEndian.Uint32(ieData[:4])
			}
		}
	}
	return acc, nil
}

// --- Helpers ---

// DecodeSUPIFromMobileIdentity extracts SUPI from a Registration Request mobile identity
// (null-scheme SUCI produced by encodeSUPI).
// Ref: TS 24.501 §9.11.3.4
func DecodeSUPIFromMobileIdentity(identity []byte) (SUPI, error) {
	if len(identity) < 9 {
		return "", fmt.Errorf("mobile identity too short (%d bytes)", len(identity))
	}
	if identity[0]&0x07 != 0x01 {
		return "", fmt.Errorf("unsupported mobile identity type 0x%02x", identity[0]&0x07)
	}
	msin, err := decodeMSIN(identity[8:])
	if err != nil {
		return "", err
	}
	// PLMN matches encodeSUPI test PLMN 001-01
	return SUPI("imsi-00101" + msin), nil
}

// encodeSUPI encodes a SUPI (e.g. "imsi-001010000000001") as a 5G mobile identity.
// Format: SUCI (Subscription Concealed Identifier) with null-scheme (no concealment).
// Ref: TS 24.501 §9.11.3.4, TS 23.003 §2.2B
func encodeSUPI(supi SUPI) []byte {
	// Simplified: encode as IMSI identity type (type=1)
	// Real 5G would use SUCI with home network public key encryption
	// For simulation we use null-scheme (scheme=0) which is SUPI in clear
	// Ref: TS 24.501 §9.11.3.4 Table 9.11.3.4.1

	// Identity type = SUCI (001), odd/even indicator
	result := []byte{0x01} // type = SUCI, even number of digits

	// Encode PLMN from SUPI "imsi-MCCMNCMSISDN"
	// For simplicity encode as fixed test PLMN 00101
	result = append(result,
		0x00, 0xF1, 0x10, // PLMN: MCC=001 MNC=01
		0x00, 0x00, // Routing Indicator: 0000
		0x00, // Protection Scheme: null-scheme
		0x00, // Home Network Public Key ID: 0
	)

	// MSIN (Mobile Subscriber Identification Number) — last 10 digits of SUPI
	s := string(supi)
	if len(s) > 5 && s[:5] == "imsi-" {
		s = s[5:]
	}
	// Keep last 10 digits as MSIN, BCD encoded
	if len(s) > 10 {
		s = s[len(s)-10:]
	}
	result = append(result, encodeMSIN(s)...)

	return result
}

// encodeMSIN encodes a digit string as BCD pairs.
func encodeMSIN(msin string) []byte {
	var result []byte
	for i := 0; i < len(msin); i += 2 {
		var b byte
		b = msin[i] - '0'
		if i+1 < len(msin) {
			b |= (msin[i+1] - '0') << 4
		} else {
			b |= 0xF0 // padding
		}
		result = append(result, b)
	}
	return result
}

// decodeMSIN decodes BCD MSIN digits from mobile identity tail bytes.
func decodeMSIN(data []byte) (string, error) {
	var digits []byte
	for _, b := range data {
		lo := b & 0x0F
		hi := (b >> 4) & 0x0F
		if lo <= 9 {
			digits = append(digits, '0'+lo)
		}
		if hi <= 9 {
			digits = append(digits, '0'+hi)
		}
	}
	if len(digits) == 0 {
		return "", fmt.Errorf("empty MSIN in mobile identity")
	}
	return string(digits), nil
}

// encodeGUTI encodes a 5G-GUTI as bytes.
// Ref: TS 24.501 §9.11.3.4 Table 9.11.3.4.3
func encodeGUTI(g *GUTI5G) []byte {
	result := make([]byte, 0, 11)

	// Identity type = 5G-GUTI (110)
	result = append(result, 0xF6) // odd indicator + type=110

	// PLMN (3 bytes BCD) — reuse same encoding as NGAP
	plmn := g.PLMN
	if len(plmn) == 5 {
		plmn = plmn[:3] + "f" + plmn[3:]
	}
	d := func(i int) byte { return plmn[i] - '0' }
	f := func(i int) byte {
		if plmn[i] == 'f' || plmn[i] == 'F' {
			return 0xF
		}
		return plmn[i] - '0'
	}
	result = append(result,
		(d(1)<<4)|d(0),
		(f(3)<<4)|d(2),
		(d(5)<<4)|d(4),
	)

	// AMF Identifier: Region (8 bits) + Set (10 bits) + Pointer (6 bits) = 3 bytes
	// Ref: TS 23.003 §2.10.1
	result = append(result, g.AMFRegion)
	result = append(result, byte(g.AMFSet>>2))
	result = append(result, byte((g.AMFSet&0x03)<<6)|(g.AMFPtr&0x3F))

	// 5G-TMSI: 32 bits
	result = append(result,
		byte(g.TMSI>>24),
		byte(g.TMSI>>16),
		byte(g.TMSI>>8),
		byte(g.TMSI),
	)

	return result
}

// decodeGUTI decodes a 5G-GUTI from bytes.
func decodeGUTI(data []byte) *GUTI5G {
	if len(data) < 10 {
		return nil
	}

	g := &GUTI5G{}

	// Skip byte 0 (identity type indicator)
	// PLMN: bytes 1-3
	b := data[1:4]
	mcc1 := b[0] & 0x0F
	mcc2 := (b[0] >> 4) & 0x0F
	mcc3 := b[1] & 0x0F
	mnc3 := (b[1] >> 4) & 0x0F
	mnc1 := b[2] & 0x0F
	mnc2 := (b[2] >> 4) & 0x0F
	if mnc3 == 0xF {
		g.PLMN = fmt.Sprintf("%d%d%d%d%d", mcc1, mcc2, mcc3, mnc1, mnc2)
	} else {
		g.PLMN = fmt.Sprintf("%d%d%d%d%d%d", mcc1, mcc2, mcc3, mnc3, mnc1, mnc2)
	}

	// AMF Region: byte 4
	g.AMFRegion = data[4]
	// AMF Set: bytes 5-6 (10 bits)
	g.AMFSet = (uint8(data[5]) << 2) | (data[6] >> 6)
	// AMF Pointer: byte 6 low 6 bits
	g.AMFPtr = data[6] & 0x3F

	// 5G-TMSI: bytes 7-10
	g.TMSI = binary.BigEndian.Uint32(data[7:11])

	return g
}

// encodeNSSAI encodes a slice list as NSSAI bytes.
// Ref: TS 24.501 §9.11.3.37
func encodeNSSAI(nssai []SNSSAI) []byte {
	var result []byte
	for _, s := range nssai {
		if s.SD == 0xFFFFFF || s.SD == 0 {
			// SST only (1 byte)
			result = append(result, 0x01, s.SST)
		} else {
			// SST + SD (4 bytes)
			result = append(result, 0x04, s.SST,
				byte(s.SD>>16), byte(s.SD>>8), byte(s.SD))
		}
	}
	return result
}

// decodeNSSAI parses NSSAI bytes into a slice list.
func decodeNSSAI(data []byte) []SNSSAI {
	var result []SNSSAI
	offset := 0
	for offset < len(data) {
		if offset >= len(data) {
			break
		}
		length := int(data[offset])
		offset++
		if offset+length > len(data) {
			break
		}
		entry := data[offset : offset+length]
		offset += length

		s := SNSSAI{SD: 0xFFFFFF}
		if len(entry) >= 1 {
			s.SST = entry[0]
		}
		if len(entry) >= 4 {
			s.SD = uint32(entry[1])<<16 | uint32(entry[2])<<8 | uint32(entry[3])
		}
		result = append(result, s)
	}
	return result
}
