// Package smf implements the Session Management Function.
//
// The SMF manages PDU sessions — it allocates IP addresses, selects UPFs,
// and installs packet forwarding rules via PFCP (N4 interface).
//
// In our simulator we implement:
//   - Nsmf_PDUSession service (TS 29.502) — N11 interface to AMF
//   - IP address pool management
//   - Session context tracking
//
// Ref: TS 23.501 §6.2.3 — SMF
// Ref: TS 29.502 — Nsmf_PDUSession API
// Ref: TS 23.502 §4.3.2 — PDU Session Establishment
package smf

import "time"

// PDUSessionType represents the type of PDU session.
// Ref: TS 23.501 §5.8.2
type PDUSessionType string

const (
	PDUSessionTypeIPv4     PDUSessionType = "IPV4"
	PDUSessionTypeIPv6     PDUSessionType = "IPV6"
	PDUSessionTypeIPv4v6   PDUSessionType = "IPV4V6"
	PDUSessionTypeEthernet PDUSessionType = "ETHERNET"
)

// PDUSessionStatus represents the lifecycle state of a PDU session.
// Ref: TS 29.502 §6.1.6.3.4
type PDUSessionStatus string

const (
	PDUSessionStatusActive   PDUSessionStatus = "ACTIVE"
	PDUSessionStatusInactive PDUSessionStatus = "INACTIVE"
	PDUSessionStatusReleased PDUSessionStatus = "RELEASED"
)

// SNssai is the Single Network Slice Selection Assistance Information.
// Ref: TS 23.003 §28.4
type SNssai struct {
	Sst int    `json:"sst"`
	Sd  string `json:"sd,omitempty"`
}

// SmContextCreateRequest is sent by the AMF to the SMF to create a session.
// This maps to the Nsmf_PDUSession_CreateSMContext request.
// Ref: TS 29.502 §6.1.6.2.2
type SmContextCreateRequest struct {
	// Supi is the UE's permanent identity.
	Supi string `json:"supi"`

	// Pei is the Permanent Equipment Identifier (IMEI).
	Pei string `json:"pei,omitempty"`

	// Gpsi is the Generic Public Subscription Identifier.
	Gpsi string `json:"gpsi,omitempty"`

	// PDUSessionID identifies this session (1-255, UE-assigned).
	// Ref: TS 24.501 §9.11.3.41
	PDUSessionID int `json:"pduSessionId"`

	// Dnn is the Data Network Name (like APN in 4G).
	// e.g. "internet", "ims"
	Dnn string `json:"dnn"`

	// SNssai is the slice this session belongs to.
	SNssai SNssai `json:"sNssai"`

	// PDUSessionType requested by the UE.
	PDUSessionType PDUSessionType `json:"pduSessionType"`

	// ServingNfID is the AMF instance ID making this request.
	ServingNfID string `json:"servingNfId"`

	// ServingNetwork is the PLMN.
	ServingNetwork string `json:"servingNetwork"`

	// N1SmMsg is the NAS SM message from the UE (PDU Session Estab Request).
	// Base64 encoded. Ref: TS 29.502 §6.1.6.2.2
	N1SmMsg string `json:"n1SmMsg,omitempty"`
}

// SmContextCreateResponse is returned by the SMF when a context is created.
// Ref: TS 29.502 §6.1.6.2.3
type SmContextCreateResponse struct {
	// SmContextRef is the URI the AMF uses to refer to this context.
	// e.g. "http://smf:8001/nsmf-pdusession/v1/sm-contexts/ctx-001"
	SmContextRef string `json:"smContextRef"`

	// PDUAddress is the IP address allocated to the UE.
	PDUAddress *PDUAddress `json:"pduAddress,omitempty"`

	// N1SmMsg is the NAS SM response to send to the UE.
	N1SmMsg string `json:"n1SmMsg,omitempty"`

	// Cause indicates why the request failed (if applicable).
	Cause string `json:"cause,omitempty"`
}

// PDUAddress holds the IP address(es) allocated to the UE.
// Ref: TS 29.502 §6.1.6.2.14
type PDUAddress struct {
	// Pdu session type determines which fields are set.
	PduSessionType PDUSessionType `json:"pduSessionType"`

	// Ipv4Addr is the allocated IPv4 address (e.g. "10.0.0.1").
	Ipv4Addr string `json:"ipv4Addr,omitempty"`

	// Ipv6Prefix is the allocated IPv6 prefix.
	Ipv6Prefix string `json:"ipv6Prefix,omitempty"`
}

// SmContext is the SMF's internal record of an active PDU session.
type SmContext struct {
	// ID is the SMF-assigned context identifier.
	ID string

	// SUPI of the UE this session belongs to.
	SUPI string

	// PDUSessionID is the UE-assigned session ID (1-255).
	PDUSessionID int

	// DNN is the Data Network Name for this session.
	DNN string

	// SNssai is the slice.
	SNssai SNssai

	// PDUSessionType (IPv4/IPv6/IPv4v6).
	PDUSessionType PDUSessionType

	// AllocatedIP is the IP address given to the UE.
	AllocatedIP string

	// Status is the current session state.
	Status PDUSessionStatus

	// AMFAddress is where to send N11 callbacks.
	AMFAddress string

	// CreatedAt is when this session was established.
	CreatedAt time.Time
}

// ErrorResponse is the problem detail format for API errors.
// Ref: TS 29.571 §5.2.6
type ErrorResponse struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}
