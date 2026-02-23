// Package gtp implements GTP-U (GPRS Tunnelling Protocol - User Plane).
//
// GTP-U carries user plane traffic (IP packets) between the gNB and UPF
// over UDP port 2152. It's defined in TS 29.281.
//
// Packet structure (TS 29.281 §5.1):
//
//	Octet 1:    Flags (version=1, PT=1, E, S, PN)
//	Octet 2:    Message Type
//	Octet 3-4:  Total Length (of payload + optional fields)
//	Octet 5-8:  TEID (Tunnel Endpoint Identifier)
//	--- optional (if E=1, S=1, or PN=1) ---
//	Octet 9-10: Sequence Number
//	Octet 11:   N-PDU Number
//	Octet 12:   Next Extension Header Type
//	--- payload ---
//	Octet 13+:  User data (inner IP packet)
//
// Ref: TS 29.281 — General Packet Radio System (GPRS) Tunnelling Protocol
//
//	User Plane (GTPv1-U)
package gtp

import (
	"encoding/binary"
	"fmt"
)

const (
	// GTPUPort is the well-known UDP port for GTP-U.
	// Ref: TS 29.281 §4.4.2
	GTPUPort = 2152

	// GTPVersion1 is the version flag for GTPv1.
	GTPVersion1 = 0x20

	// ProtocolTypeBit indicates GTP (not GTP') when set.
	ProtocolTypeBit = 0x10

	// MinHeaderLen is the minimum GTP-U header size (no optional fields).
	MinHeaderLen = 8

	// ExtHeaderLen is the header size when S/E/PN flags are set.
	ExtHeaderLen = 12
)

// Message Type values for GTP-U.
// Ref: TS 29.281 §7.1
const (
	MsgTypeEchoRequest         = 0x01
	MsgTypeEchoResponse        = 0x02
	MsgTypeErrorIndication     = 0x1A
	MsgTypeSupportedExtHeaders = 0x1F
	MsgTypeEndMarker           = 0xFE
	MsgTypeGPDU                = 0xFF // G-PDU: carries a user data packet
)

// Header represents a decoded GTP-U packet header.
// Ref: TS 29.281 §5.1
type Header struct {
	// Flags byte fields
	Version       uint8 // Always 1 for GTPv1
	ProtocolType  uint8 // 1 = GTP, 0 = GTP'
	ExtHeaderFlag bool  // Extension header present
	SeqNumFlag    bool  // Sequence number present
	NPDUFlag      bool  // N-PDU number present

	// Mandatory fields
	MessageType uint8
	Length      uint16 // Length of payload + optional header fields
	TEID        uint32 // Tunnel Endpoint Identifier

	// Optional fields (present when any of E/S/PN flags set)
	SequenceNumber uint16
	NPDUNumber     uint8
	NextExtHeader  uint8
}

// Packet is a fully decoded GTP-U packet.
type Packet struct {
	Header  Header
	Payload []byte // The inner IP packet (or echo request/response body)
}

// Encode serialises a GTP-U packet to bytes.
// Ref: TS 29.281 §5.1
func Encode(p *Packet) ([]byte, error) {
	h := &p.Header

	// Determine if we need the extended header (4 extra bytes)
	useExtHeader := h.ExtHeaderFlag || h.SeqNumFlag || h.NPDUFlag

	headerLen := MinHeaderLen
	if useExtHeader {
		headerLen = ExtHeaderLen
	}

	// Length field = payload length + optional header fields (not the first 8 bytes)
	// Ref: TS 29.281 §5.1
	extLen := 0
	if useExtHeader {
		extLen = 4 // seqnum(2) + npdu(1) + nextext(1)
	}
	totalLength := uint16(len(p.Payload) + extLen)

	buf := make([]byte, headerLen+len(p.Payload))

	// Byte 0: Flags
	flags := GTPVersion1 | ProtocolTypeBit
	if h.ExtHeaderFlag {
		flags |= 0x04
	}
	if h.SeqNumFlag {
		flags |= 0x02
	}
	if h.NPDUFlag {
		flags |= 0x01
	}
	buf[0] = byte(flags)

	// Byte 1: Message Type
	buf[1] = h.MessageType

	// Bytes 2-3: Length
	binary.BigEndian.PutUint16(buf[2:4], totalLength)

	// Bytes 4-7: TEID
	binary.BigEndian.PutUint32(buf[4:8], h.TEID)

	if useExtHeader {
		// Bytes 8-9: Sequence Number
		binary.BigEndian.PutUint16(buf[8:10], h.SequenceNumber)
		// Byte 10: N-PDU Number
		buf[10] = h.NPDUNumber
		// Byte 11: Next Extension Header Type (0x00 = no extension)
		buf[11] = h.NextExtHeader
	}

	// Payload (inner IP packet)
	copy(buf[headerLen:], p.Payload)

	return buf, nil
}

// Decode parses a GTP-U packet from raw bytes.
// Ref: TS 29.281 §5.1
func Decode(data []byte) (*Packet, error) {
	if len(data) < MinHeaderLen {
		return nil, fmt.Errorf("GTP-U packet too short: %d bytes (min %d)",
			len(data), MinHeaderLen)
	}

	h := Header{}

	// Byte 0: Flags
	flags := data[0]
	h.Version = (flags >> 5) & 0x07
	h.ProtocolType = (flags >> 4) & 0x01
	h.ExtHeaderFlag = (flags & 0x04) != 0
	h.SeqNumFlag = (flags & 0x02) != 0
	h.NPDUFlag = (flags & 0x01) != 0

	if h.Version != 1 {
		return nil, fmt.Errorf("unsupported GTP version: %d (want 1)", h.Version)
	}

	// Byte 1: Message Type
	h.MessageType = data[1]

	// Bytes 2-3: Length
	h.Length = binary.BigEndian.Uint16(data[2:4])

	// Bytes 4-7: TEID
	h.TEID = binary.BigEndian.Uint32(data[4:8])

	// Optional fields
	payloadOffset := MinHeaderLen
	if h.ExtHeaderFlag || h.SeqNumFlag || h.NPDUFlag {
		if len(data) < ExtHeaderLen {
			return nil, fmt.Errorf("GTP-U extended header too short: %d bytes", len(data))
		}
		h.SequenceNumber = binary.BigEndian.Uint16(data[8:10])
		h.NPDUNumber = data[10]
		h.NextExtHeader = data[11]
		payloadOffset = ExtHeaderLen
	}

	// Validate length
	expectedPayloadLen := int(h.Length) - (payloadOffset - MinHeaderLen)
	if payloadOffset+expectedPayloadLen > len(data) {
		return nil, fmt.Errorf("GTP-U payload length mismatch: header says %d bytes, got %d",
			expectedPayloadLen, len(data)-payloadOffset)
	}

	payload := make([]byte, expectedPayloadLen)
	copy(payload, data[payloadOffset:payloadOffset+expectedPayloadLen])

	return &Packet{Header: h, Payload: payload}, nil
}

// NewGPDU creates a G-PDU packet (the common case — wrapping an IP packet).
// Ref: TS 29.281 §5.1
func NewGPDU(teid uint32, innerIPPacket []byte) *Packet {
	return &Packet{
		Header: Header{
			Version:      1,
			ProtocolType: 1,
			MessageType:  MsgTypeGPDU,
			TEID:         teid,
		},
		Payload: innerIPPacket,
	}
}

// NewEchoRequest creates a GTP-U Echo Request for path management.
// Ref: TS 29.281 §7.2.1
func NewEchoRequest(seqNum uint16) *Packet {
	return &Packet{
		Header: Header{
			Version:        1,
			ProtocolType:   1,
			MessageType:    MsgTypeEchoRequest,
			TEID:           0, // Always 0 for echo
			SeqNumFlag:     true,
			SequenceNumber: seqNum,
		},
		Payload: []byte{0x00}, // Recovery IE (mandatory in echo)
	}
}

// NewEchoResponse creates a GTP-U Echo Response.
// Ref: TS 29.281 §7.2.2
func NewEchoResponse(seqNum uint16) *Packet {
	return &Packet{
		Header: Header{
			Version:        1,
			ProtocolType:   1,
			MessageType:    MsgTypeEchoResponse,
			TEID:           0,
			SeqNumFlag:     true,
			SequenceNumber: seqNum,
		},
		Payload: []byte{0x0E, 0x01, 0x00}, // Recovery IE
	}
}

// String returns a human-readable summary of a GTP-U packet.
func (p *Packet) String() string {
	msgName := map[uint8]string{
		MsgTypeEchoRequest:     "EchoRequest",
		MsgTypeEchoResponse:    "EchoResponse",
		MsgTypeErrorIndication: "ErrorIndication",
		MsgTypeEndMarker:       "EndMarker",
		MsgTypeGPDU:            "G-PDU",
	}
	name, ok := msgName[p.Header.MessageType]
	if !ok {
		name = fmt.Sprintf("Unknown(0x%02X)", p.Header.MessageType)
	}
	return fmt.Sprintf("GTP-U[%s TEID=0x%08X payload=%d bytes]",
		name, p.Header.TEID, len(p.Payload))
}
