// nas_handler.go — AMF handlers for NAS-carrying NGAP messages.
//
// These handlers process NGAP messages that carry NAS payloads:
//   - InitialUEMessage    (gNB → AMF, first message from a UE)
//   - UplinkNASTransport  (gNB → AMF, subsequent UE→AMF NAS messages)
//
// The NAS payload inside is decoded and routed to the appropriate
// NAS procedure handler (Registration, Service Request, etc.)
//
// Ref: TS 38.413 §9.2.5.1 — Initial UE Message
// Ref: TS 38.413 §9.2.5.3 — Uplink NAS Transport
// Ref: TS 23.502 §4.2.2   — Registration procedure
package amf

import (
	"fmt"
	"net"
	"time"

	"github.com/free5gc/ngap/ngapType"

	"github.com/afroash/5g-sim/internal/nas"
	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// HandleInitialUEMessage processes the first NGAP message from a UE.
//
// The gNB sends this when a UE first makes contact. It contains:
//   - The NAS message from the UE (typically Registration Request)
//   - The UE's location info (TAI, NR-CGI)
//   - A RAN UE NGAP ID assigned by the gNB
//
// The AMF decodes the NAS payload and starts the appropriate procedure.
//
// Ref: TS 38.413 §9.2.5.1
func (a *AMF) HandleInitialUEMessage(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Printf("[AMF] Received InitialUEMessage from %s\n", conn.RemoteAddr())

	msg := pdu.InitiatingMessage.Value.InitialUEMessage
	if msg == nil {
		fmt.Println("[AMF] InitialUEMessage is nil")
		return
	}

	var (
		ranUeNgapID int64
		nasPayload  []byte
		foundRanID  bool
		foundNAS    bool
	)

	// Extract IEs — Ref: TS 38.413 Table 9.2.5.1-1
	for _, ie := range msg.ProtocolIEs.List {
		switch ie.Id.Value {

		// RAN UE NGAP ID — the gNB's local identifier for this UE
		// Ref: TS 38.413 §9.3.3.2
		case ngapType.ProtocolIEIDRANUENGAPID:
			foundRanID = true
			if ie.Value.RANUENGAPID != nil {
				ranUeNgapID = ie.Value.RANUENGAPID.Value
			}

		// NAS-PDU — the actual NAS message from the UE
		// Ref: TS 38.413 §9.3.2.1
		case ngapType.ProtocolIEIDNASPDU:
			foundNAS = true
			if ie.Value.NASPDU != nil {
				nasPayload = ie.Value.NASPDU.Value
			}

		// UserLocationInformation — where the UE is (TAI + cell)
		case ngapType.ProtocolIEIDUserLocationInformation:
			if ie.Value.UserLocationInformation != nil {
				fmt.Printf("[AMF]   UE location info present (type=%d)\n",
					ie.Value.UserLocationInformation.Present)
			}
		}
	}

	if !foundRanID || !foundNAS {
		fmt.Println("[AMF] InitialUEMessage missing mandatory IEs")
		return
	}

	fmt.Printf("[AMF]   RAN UE NGAP ID: %d, NAS payload: %d bytes\n",
		ranUeNgapID, len(nasPayload))

	// Find which RAN this connection belongs to
	ran, ok := a.GetRAN(conn)
	if !ok {
		fmt.Printf("[AMF] Unknown RAN for connection %s\n", conn.RemoteAddr())
		return
	}

	// Route the NAS message to the right handler (no UE context yet on first message).
	a.routeNASMessage(conn, ran, nil, ranUeNgapID, nasPayload)
}

// HandleUplinkNASTransport processes subsequent NAS messages from a UE
// (after the initial message). Used for Registration Complete, etc.
//
// Ref: TS 38.413 §9.2.5.3
func (a *AMF) HandleUplinkNASTransport(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Printf("[AMF] Received UplinkNASTransport from %s\n", conn.RemoteAddr())

	msg := pdu.InitiatingMessage.Value.UplinkNASTransport
	if msg == nil {
		return
	}

	var (
		amfUeNgapID int64
		nasPayload  []byte
	)

	for _, ie := range msg.ProtocolIEs.List {
		switch ie.Id.Value {
		case ngapType.ProtocolIEIDAMFUENGAPID:
			if ie.Value.AMFUENGAPID != nil {
				amfUeNgapID = ie.Value.AMFUENGAPID.Value
			}
		case ngapType.ProtocolIEIDNASPDU:
			if ie.Value.NASPDU != nil {
				nasPayload = ie.Value.NASPDU.Value
			}
		}
	}

	// Look up UE context by AMF UE NGAP ID
	ue, ok := a.ues.GetByNgapID(amfUeNgapID)
	if !ok {
		fmt.Printf("[AMF] UplinkNASTransport: unknown AMF UE NGAP ID %d\n", amfUeNgapID)
		return
	}

	ran, ok := a.GetRAN(conn)
	if !ok {
		return
	}

	a.routeNASMessage(conn, ran, ue, ue.RANUeNgapID, nasPayload)
}

// routeNASMessage decodes a NAS message header and dispatches to the
// appropriate procedure handler.
func (a *AMF) routeNASMessage(conn net.Conn, ran *RAN, ue *UEContext, ranUeNgapID int64, nasPayload []byte) {
	msg, err := nas.Decode(nasPayload)
	if err != nil {
		fmt.Printf("[AMF] NAS decode error: %v\n", err)
		return
	}

	fmt.Printf("[AMF]   NAS EPD=0x%02X MsgType=0x%02X\n", msg.EPD, msg.MessageType)

	switch msg.MessageType {
	case nas.MsgTypeRegistrationRequest:
		a.handleRegistrationRequest(conn, ran, ranUeNgapID, msg)
	case nas.MsgTypeRegistrationComplete:
		a.handleRegistrationComplete(conn, ue, msg)

	// NAS SM messages arrive wrapped in UL NAS Transport (MM type 0x67)
	// Ref: TS 24.501 §8.2.14
	case 0x67: // UL NAS Transport carrying SM container
		a.handleULNASTransportSM(conn, ran, msg)

	default:
		fmt.Printf("[AMF]   Unhandled NAS message type: 0x%02X\n", msg.MessageType)
	}
}

// handleRegistrationRequest processes a NAS Registration Request.
//
// Steps (TS 23.502 §4.2.2):
//  1. Decode the Registration Request IEs
//  2. Create a UE context
//  3. Assign a 5G-GUTI
//  4. Send Registration Accept wrapped in DownlinkNASTransport
//
// We skip authentication and security for now — those come in a later phase.
// Ref: TS 23.502 §4.2.2.2
func (a *AMF) handleRegistrationRequest(conn net.Conn, ran *RAN, ranUeNgapID int64, msg *nas.Message) {
	fmt.Println("[AMF]   Processing Registration Request")
	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"InitialUEMessage (Registration Request)", "TS 38.413 §9.2.5.1")
	}

	req, err := nas.DecodeRegistrationRequest(msg.Payload)
	if err != nil {
		fmt.Printf("[AMF]   Failed to decode Registration Request: %v\n", err)
		return
	}

	fmt.Printf("[AMF]   Registration type: %d  Follow-on: %v\n",
		req.RegistrationType, req.FollowOnRequest)

	supi, err := nas.DecodeSUPIFromMobileIdentity(req.MobileIdentity)
	if err != nil {
		fmt.Printf("[AMF]   Failed to decode SUPI: %v\n", err)
		nasReject := nas.BuildRegistrationReject(nas.CauseUEIdentityNotDerived)
		_ = a.sendDownlinkNASTransport(conn, 0, ranUeNgapID, nasReject)
		return
	}
	fmt.Printf("[AMF]   SUPI: %s\n", supi)

	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeAMF, seqdiag.NodeUDM,
			"Nudm_UECM_Registration", "TS 29.503 §5.3.2",
			"supi", string(supi))
	}
	subData, err := a.udmRegister(string(supi))
	if err != nil {
		fmt.Printf("[AMF]   UDM rejected registration: %v\n", err)
		if a.Hub != nil {
			a.Hub.Procedure(seqdiag.NodeUDM, seqdiag.NodeAMF,
				"404 Not Found (unknown subscriber)", "TS 29.503")
		}
		nasReject := nas.BuildRegistrationReject(nas.CauseIllegalUE)
		_ = a.sendDownlinkNASTransport(conn, 0, ranUeNgapID, nasReject)
		return
	}
	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeUDM, seqdiag.NodeAMF,
			"201 Created (subscription confirmed)", "TS 29.503")
	}

	// Assign a new 5G-GUTI for this UE
	guti, err := a.AllocateGUTI()
	if err != nil {
		fmt.Printf("[AMF]   Failed to allocate GUTI: %v\n", err)
		return
	}

	// Build allowed NSSAI — grant what the UE requested (simplified)
	allowedNSSAI := []nas.SNSSAI{{SST: 1, SD: 0xFFFFFF}} // eMBB
	if len(req.RequestedNSSAI) > 0 {
		allowedNSSAI = req.RequestedNSSAI
	}

	// Create UE context in the AMF
	ue := &UEContext{
		SUPI:             string(supi),
		GUTI:             guti,
		RAN:              ran,
		RANUeNgapID:      ranUeNgapID,
		AllowedNSSAI:     allowedNSSAI,
		AllowedDnns:      subData.AllowedDnns,
		RegistrationType: req.RegistrationType,
		State:            UEStateRegistering,
		RegisteredAt:     time.Now(),
	}
	a.ues.Add(ue)

	fmt.Printf("[AMF]   Created UE context: AMF-UE-NGAP-ID=%d GUTI-TMSI=0x%08X\n",
		ue.AMFUeNgapID, guti.TMSI)

	// Build NAS Registration Accept
	nasAccept := nas.BuildRegistrationAccept(
		nas.RegistrationResult3GPP,
		guti,
		allowedNSSAI,
	)

	// Wrap in NGAP DownlinkNASTransport and send to gNB
	// Ref: TS 38.413 §9.2.5.2
	if err := a.sendDownlinkNASTransport(conn, ue.AMFUeNgapID, ranUeNgapID, nasAccept); err != nil {
		fmt.Printf("[AMF]   Failed to send DownlinkNASTransport: %v\n", err)
		return
	}

	fmt.Printf("[AMF]   Registration Accept sent → UE TMSI=0x%08X ✓\n", guti.TMSI)
}

// handleRegistrationComplete processes NAS Registration Complete from the UE.
// The UE sends this to confirm it has stored the new GUTI.
// Ref: TS 23.502 §4.2.2.2.2 step 18
func (a *AMF) handleRegistrationComplete(_ net.Conn, ue *UEContext, msg *nas.Message) {
	_ = msg
	if ue == nil {
		fmt.Println("[AMF]   Registration Complete (no UE context)")
		return
	}
	ue.State = UEStateRegistered
	fmt.Printf("[AMF]   Registration Complete — UE %s REGISTERED ✓\n", ue.SUPI)
	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"N2: Registration Complete", "TS 38.413 §9.2.5.3")
	}
}

// sendDownlinkNASTransport wraps a NAS message in an NGAP DownlinkNASTransport
// and sends it to the gNB for forwarding to the UE.
//
// Ref: TS 38.413 §9.2.5.2
func (a *AMF) sendDownlinkNASTransport(conn net.Conn, amfUeNgapID, ranUeNgapID int64, nasPayload []byte) error {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeDownlinkNASTransport
	pdu.InitiatingMessage.Criticality.Value = ngapType.CriticalityPresentIgnore
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentDownlinkNASTransport

	dlNAS := ngapType.DownlinkNASTransport{}

	// IE: AMF UE NGAP ID
	{
		ie := ngapType.DownlinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDAMFUENGAPID
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.DownlinkNASTransportIEsPresentAMFUENGAPID
		ie.Value.AMFUENGAPID = new(ngapType.AMFUENGAPID)
		ie.Value.AMFUENGAPID.Value = amfUeNgapID
		dlNAS.ProtocolIEs.List = append(dlNAS.ProtocolIEs.List, ie)
	}

	// IE: RAN UE NGAP ID
	{
		ie := ngapType.DownlinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDRANUENGAPID
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.DownlinkNASTransportIEsPresentRANUENGAPID
		ie.Value.RANUENGAPID = new(ngapType.RANUENGAPID)
		ie.Value.RANUENGAPID.Value = ranUeNgapID
		dlNAS.ProtocolIEs.List = append(dlNAS.ProtocolIEs.List, ie)
	}

	// IE: NAS-PDU — the NAS message to deliver to the UE
	{
		ie := ngapType.DownlinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDNASPDU
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.DownlinkNASTransportIEsPresentNASPDU
		ie.Value.NASPDU = new(ngapType.NASPDU)
		ie.Value.NASPDU.Value = nasPayload
		dlNAS.ProtocolIEs.List = append(dlNAS.ProtocolIEs.List, ie)
	}

	pdu.InitiatingMessage.Value.DownlinkNASTransport = &dlNAS

	data, err := ngapbuilder.EncodeNGAP(pdu)
	if err != nil {
		return fmt.Errorf("encode DownlinkNASTransport: %w", err)
	}

	if err := a.sendNGAP(conn, data); err != nil {
		return fmt.Errorf("send DownlinkNASTransport: %w", err)
	}

	fmt.Printf("[AMF]   DownlinkNASTransport sent (%d bytes)\n", len(data))
	return nil
}

// handleULNASTransportSM unwraps a NAS UL NAS Transport (MM) message
// and dispatches the carried SM payload to the right SM handler.
//
// NAS UL NAS Transport structure (TS 24.501 §8.2.14):
//
//	[0] EPD, [1] SecHdr, [2] MsgType=0x67
//	[3] Payload container type
//	[4-5] Payload container length
//	[6..] Payload container (the SM message)
//	... PDU Session ID IE follows
func (a *AMF) handleULNASTransportSM(conn net.Conn, ran *RAN, msg *nas.Message) {
	payload := msg.Payload
	if len(payload) < 4 {
		fmt.Println("[AMF]   UL NAS Transport payload too short")
		return
	}

	containerType := payload[0]
	containerLen := int(payload[1])<<8 | int(payload[2])

	if 3+containerLen > len(payload) {
		fmt.Println("[AMF]   UL NAS Transport: container length mismatch")
		return
	}

	smPayload := payload[3 : 3+containerLen]

	// Parse PDU Session ID from IEs after the container
	var pduSessionID uint8
	offset := 3 + containerLen
	for offset < len(payload)-1 {
		iei := payload[offset]
		offset++
		if iei == 0x12 && offset < len(payload) { // PDU Session ID IEI
			pduSessionID = payload[offset]
			offset++
		} else {
			offset++ // skip value
		}
	}

	fmt.Printf("[AMF]   UL NAS Transport: containerType=%d smLen=%d pduSessionID=%d\n",
		containerType, len(smPayload), pduSessionID)

	if len(smPayload) < 4 {
		return
	}

	smMsgType := smPayload[3]

	// Find UE context — use the RAN's most recently registered UE (simplified)
	// In a full implementation we'd look up by RAN UE NGAP ID
	var ue *UEContext
	a.ues.mu.RLock()
	for _, u := range a.ues.byNgapID {
		if u.RAN == ran {
			ue = u
			break
		}
	}
	a.ues.mu.RUnlock()

	if ue == nil {
		fmt.Println("[AMF]   UL NAS Transport: no UE context found for this RAN")
		return
	}

	switch smMsgType {
	case nas.MsgTypePDUSessionEstablishmentRequest:
		a.HandlePDUSessionEstablishmentRequest(conn, ue, smPayload)
	default:
		fmt.Printf("[AMF]   Unhandled SM message type: 0x%02X\n", smMsgType)
	}
}
