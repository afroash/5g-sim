// Package nas implements the 5G NAS (Non-Access Stratum) protocol.
//
// NAS runs between the UE and the AMF. The gNB is transparent to it —
// it carries NAS messages inside NGAP without interpreting them.
//
// This package covers the Registration procedure subset of TS 24.501:
//   - Registration Request   (UE → AMF)
//   - Registration Accept    (AMF → UE)
//   - Registration Complete  (UE → AMF)
//   - Registration Reject    (AMF → UE)
//
// NAS message structure (TS 24.501 §9.1.1):
//
//	Byte 0:    Extended Protocol Discriminator (EPD)
//	Byte 1:    Security Header Type
//	Byte 2-3:  Message Type
//	Byte 4+:   Information Elements (IEs)
//
// Ref: TS 24.501 — Non-Access-Stratum (NAS) protocol for 5G System
package nas

// Extended Protocol Discriminator values.
// Ref: TS 24.007 §11.2.3.1
const (
	EPD5GSMobilityManagement = 0x7E // 5GS Mobility Management (MM)
	EPD5GSSessionManagement  = 0x2E // 5GS Session Management (SM)
)

// Security Header Type values.
// Ref: TS 24.501 §9.3.1
const (
	SecurityHeaderTypePlain                = 0x00 // No security — used for initial messages
	SecurityHeaderTypeIntegrityProtected   = 0x01
	SecurityHeaderTypeIntegrityAndCiphered = 0x02
)

// Message Type values for 5GS Mobility Management.
// Ref: TS 24.501 §9.7 Table 9.7.1
const (
	MsgTypeRegistrationRequest                = 0x41
	MsgTypeRegistrationAccept                 = 0x42
	MsgTypeRegistrationComplete               = 0x43
	MsgTypeRegistrationReject                 = 0x44
	MsgTypeDeregistrationRequestUEOriginating = 0x45
	MsgTypeServiceRequest                     = 0x4C
	MsgTypeIdentityRequest                    = 0x5B
	MsgTypeIdentityResponse                   = 0x5C
	MsgTypeAuthenticationRequest              = 0x56
	MsgTypeAuthenticationResponse             = 0x57
)

// Registration Type values.
// Ref: TS 24.501 §9.11.3.7
const (
	RegistrationTypeInitialRegistration   = 0x01
	RegistrationTypeMobilityRegistration  = 0x02
	RegistrationTypePeriodicRegistration  = 0x03
	RegistrationTypeEmergencyRegistration = 0x04
)

// Follow-on Request bit — UE signals it has more to send after registration.
// Ref: TS 24.501 §9.11.3.7
const (
	FollowOnRequestPending   = 0x08
	FollowOnRequestNoPending = 0x00
)

// 5GS Registration Result values.
// Ref: TS 24.501 §9.11.3.6
const (
	RegistrationResult3GPP           = 0x01
	RegistrationResultNon3GPP        = 0x02
	RegistrationResult3GPPAndNon3GPP = 0x03
)

// Cause values for Registration Reject.
// Ref: TS 24.501 §9.11.3.2
const (
	CauseIllegalUE                 = 0x03
	CauseIllegalME                 = 0x06
	CauseFiveGSServicesNotAllowed  = 0x07
	CauseUEIdentityNotDerived      = 0x09
	CauseImplicitlyDeregistered    = 0x0A
	CausePLMNNotAllowed            = 0x0B
	CauseTrackingAreaNotAllowed    = 0x0C
	CauseRoamingNotAllowedInTA     = 0x0D
	CauseNoSuitableCellsInTA       = 0x0F
	CauseCongestion                = 0x16
	CauseNotAuthorizedForThisCSG   = 0x19
	CauseInsufficientResources     = 0x1A
	CauseServiceOptionNotSupported = 0x20
)

// IE Type IDs for optional IEs in Registration messages.
// Ref: TS 24.501 §9.11 — IEI values
const (
	IEI5GSGUTI                                     = 0x77
	IEIAllowedNSSAI                                = 0x15
	IEIConfiguredNSSAI                             = 0x31
	IEINetworkSlicingIndication                    = 0x9
	IEIT3512Value                                  = 0x5E // Periodic registration timer
	IEINonCurrentNativeNASKeySetIdentifier         = 0xC
	IEI5GSDRXParameters                            = 0x51
	IEIEAPMessage                                  = 0x78
	IEIOperatorDefinedAccessCategoryDefinitions    = 0x76
	IEINegotiatedExtendedProtocolDataUnitSessionID = 0x60
	IEITAIList                                     = 0x54
	IEIMobileIdentity                              = 0x77
)

// SUPI type — Subscription Permanent Identifier.
// In 5G, SUPI replaces the 4G IMSI.
// Ref: TS 23.003 §2.2B
type SUPI string

// GUTI5G is the 5G Globally Unique Temporary Identifier.
// Assigned by the AMF to avoid transmitting SUPI over the air.
//
// Structure: MCC + MNC + AMF Region + AMF Set + AMF Pointer + TMSI
// Ref: TS 23.003 §2.10
type GUTI5G struct {
	PLMN      string // e.g. "00101"
	AMFRegion uint8
	AMFSet    uint8
	AMFPtr    uint8
	TMSI      uint32 // Temporary Mobile Subscriber Identity (32-bit)
}

// SNSSAI is the Single Network Slice Selection Assistance Information.
// Ref: TS 23.003 §28.4
type SNSSAI struct {
	SST uint8  // Slice/Service Type (1=eMBB, 2=URLLC, 3=MIoT)
	SD  uint32 // Slice Differentiator (24-bit, optional — 0xFFFFFF = not set)
}

// UESecurityCapability holds the UE's supported security algorithms.
// Ref: TS 24.501 §9.11.3.54
type UESecurityCapability struct {
	NEA0 bool // Null ciphering
	NIA2 bool // SNOW 3G integrity
	NEA2 bool // AES ciphering
	NIA0 bool // Null integrity (emergency only)
}
