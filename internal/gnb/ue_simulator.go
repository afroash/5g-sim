// ue_simulator.go — Simulates a UE connecting through the gNB.
//
// In a real network, UE messages arrive over the radio interface and
// the gNB forwards them transparently to the AMF wrapped in NGAP.
// Here we generate the NAS messages directly and wrap them ourselves.
//
// Flow:
//  1. gNB builds a NAS Registration Request on behalf of the simulated UE
//  2. Wraps it in NGAP InitialUEMessage → sends to AMF
//  3. AMF responds with NGAP DownlinkNASTransport carrying Registration Accept
//  4. gNB unwraps and decodes the NAS Registration Accept
//  5. gNB sends NGAP UplinkNASTransport carrying NAS Registration Complete
//
// Ref: TS 38.413 §9.2.5.1 — Initial UE Message
// Ref: TS 38.413 §9.2.5.2 — Downlink NAS Transport
// Ref: TS 38.413 §9.2.5.3 — Uplink NAS Transport
package gnb

import (
	"fmt"
	"net"
	"time"

	"github.com/free5gc/ngap/ngapType"

	"github.com/afroash/5g-sim/internal/nas"
	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
)

// SimulatedUE represents a simulated UE being served by this gNB.
type SimulatedUE struct {
	SUPI        nas.SUPI
	RanUeNgapID int64       // gNB's local ID for this UE
	GUTI        *nas.GUTI5G // assigned after registration
}

// StartUERegistration triggers a simulated UE to register with the network.
// Must be called after NG Setup is complete (g.IsSetup() == true).
//
// Ref: TS 23.502 §4.2.2
func (g *GNB) StartUERegistration(supi nas.SUPI) error {
	if !g.IsSetup() {
		return fmt.Errorf("gNB is not set up — complete NG Setup first")
	}

	cfg := g.Config()

	// Assign a RAN UE NGAP ID — the gNB's local handle for this UE
	// In a real gNB this is allocated from a pool
	ranUeNgapID := int64(1)

	fmt.Printf("[gNB] Initiating UE registration for SUPI: %s\n", supi)

	// Step 1: Build NAS Registration Request
	nasReq := nas.BuildRegistrationRequest(
		supi,
		nas.RegistrationTypeInitialRegistration,
		true, // follow-on request — we have PDU session to set up after
	)

	fmt.Printf("[gNB]   NAS Registration Request built (%d bytes)\n", len(nasReq))

	// Step 2: Wrap in NGAP InitialUEMessage and send to AMF
	// Ref: TS 38.413 §9.2.5.1
	return g.sendInitialUEMessage(ranUeNgapID, nasReq, cfg.TAC, cfg.PLMN)
}

// sendInitialUEMessage wraps a NAS payload in an NGAP InitialUEMessage.
// Ref: TS 38.413 §9.2.5.1
func (g *GNB) sendInitialUEMessage(ranUeNgapID int64, nasPayload []byte, tac uint32, plmn string) error {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeInitialUEMessage
	pdu.InitiatingMessage.Criticality.Value = ngapType.CriticalityPresentIgnore
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentInitialUEMessage

	initUE := ngapType.InitialUEMessage{}

	// IE: RAN UE NGAP ID — gNB's local UE identifier
	// Ref: TS 38.413 §9.3.3.2
	{
		ie := ngapType.InitialUEMessageIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDRANUENGAPID
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.InitialUEMessageIEsPresentRANUENGAPID
		ie.Value.RANUENGAPID = new(ngapType.RANUENGAPID)
		ie.Value.RANUENGAPID.Value = ranUeNgapID
		initUE.ProtocolIEs.List = append(initUE.ProtocolIEs.List, ie)
	}

	// IE: NAS-PDU — the UE's NAS message
	// Ref: TS 38.413 §9.3.2.1
	{
		ie := ngapType.InitialUEMessageIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDNASPDU
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.InitialUEMessageIEsPresentNASPDU
		ie.Value.NASPDU = new(ngapType.NASPDU)
		ie.Value.NASPDU.Value = nasPayload
		initUE.ProtocolIEs.List = append(initUE.ProtocolIEs.List, ie)
	}

	// IE: UserLocationInformation — where the UE is
	// Ref: TS 38.413 §9.3.1.15
	{
		ie := ngapType.InitialUEMessageIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDUserLocationInformation
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.InitialUEMessageIEsPresentUserLocationInformation

		locInfo := ngapType.UserLocationInformation{}
		locInfo.Present = ngapType.UserLocationInformationPresentUserLocationInformationNR

		nrInfo := ngapType.UserLocationInformationNR{}

		// PLMN + TAC for the NR-TAI
		// Field is TAI (not NRTAI) in free5gc library
		tacBytes := []byte{byte(tac >> 16), byte(tac >> 8), byte(tac)}
		nrInfo.TAI.TAC.Value = tacBytes
		nrInfo.TAI.PLMNIdentity.Value = encodePLMNBytes(plmn)

		// NR-CGI also requires a PLMN — use same value
		// Ref: TS 38.413 §9.3.1.7
		nrInfo.NRCGI.PLMNIdentity.Value = encodePLMNBytes(plmn)
		// NRCellIdentity: 36-bit cell ID — use a fixed value for simulation
		nrInfo.NRCGI.NRCellIdentity.Value.Bytes = []byte{0x00, 0x00, 0x00, 0x00, 0x10}
		nrInfo.NRCGI.NRCellIdentity.Value.BitLength = 36

		locInfo.UserLocationInformationNR = &nrInfo
		ie.Value.UserLocationInformation = &locInfo
		initUE.ProtocolIEs.List = append(initUE.ProtocolIEs.List, ie)
	}

	// IE: RRCEstablishmentCause — why the UE is connecting
	// Ref: TS 38.413 §9.3.1.10 — mo-Signalling (3) is typical for registration
	{
		ie := ngapType.InitialUEMessageIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDRRCEstablishmentCause
		ie.Criticality.Value = ngapType.CriticalityPresentIgnore
		ie.Value.Present = ngapType.InitialUEMessageIEsPresentRRCEstablishmentCause
		ie.Value.RRCEstablishmentCause = new(ngapType.RRCEstablishmentCause)
		ie.Value.RRCEstablishmentCause.Value = ngapType.RRCEstablishmentCausePresentMoSignalling
		initUE.ProtocolIEs.List = append(initUE.ProtocolIEs.List, ie)
	}

	pdu.InitiatingMessage.Value.InitialUEMessage = &initUE

	data, err := ngapbuilder.EncodeNGAP(pdu)
	if err != nil {
		return fmt.Errorf("encode InitialUEMessage: %w", err)
	}

	if err := g.Send(data); err != nil {
		return fmt.Errorf("send InitialUEMessage: %w", err)
	}

	fmt.Printf("[gNB]   InitialUEMessage sent (%d bytes)\n", len(data))
	return nil
}

// HandleDownlinkNASTransport processes a NAS message from the AMF destined for a UE.
// The gNB forwards it to the UE — here we just decode and log it.
//
// Ref: TS 38.413 §9.2.5.2
func (g *GNB) HandleDownlinkNASTransport(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Println("[gNB] Received DownlinkNASTransport from AMF")

	msg := pdu.InitiatingMessage.Value.DownlinkNASTransport
	if msg == nil {
		return
	}

	var (
		amfUeNgapID int64
		ranUeNgapID int64
		nasPayload  []byte
	)

	for _, ie := range msg.ProtocolIEs.List {
		switch ie.Id.Value {
		case ngapType.ProtocolIEIDAMFUENGAPID:
			if ie.Value.AMFUENGAPID != nil {
				amfUeNgapID = ie.Value.AMFUENGAPID.Value
			}
		case ngapType.ProtocolIEIDRANUENGAPID:
			if ie.Value.RANUENGAPID != nil {
				ranUeNgapID = ie.Value.RANUENGAPID.Value
			}
		case ngapType.ProtocolIEIDNASPDU:
			if ie.Value.NASPDU != nil {
				nasPayload = ie.Value.NASPDU.Value
			}
		}
	}

	fmt.Printf("[gNB]   AMF-UE-NGAP-ID=%d RAN-UE-NGAP-ID=%d NAS=%d bytes\n",
		amfUeNgapID, ranUeNgapID, len(nasPayload))

	// Decode the NAS message
	nasMsg, err := nas.Decode(nasPayload)
	if err != nil {
		fmt.Printf("[gNB]   NAS decode error: %v\n", err)
		return
	}

	switch nasMsg.MessageType {
	case nas.MsgTypeRegistrationAccept:
		g.handleRegistrationAccept(conn, amfUeNgapID, ranUeNgapID, nasMsg)
	case 0x68: // DL NAS Transport carrying SM container (PDU session response)
		g.handlePDUSessionAccept(nasPayload)
	default:
		fmt.Printf("[gNB]   NAS message type: 0x%02X (unhandled)\n", nasMsg.MessageType)
	}
}

// handleRegistrationAccept processes a NAS Registration Accept delivered by the AMF.
// Decodes the GUTI, logs it, then sends Registration Complete.
//
// Ref: TS 24.501 §8.2.7 / TS 23.502 §4.2.2.2.2 step 17
func (g *GNB) handleRegistrationAccept(conn net.Conn, amfUeNgapID, ranUeNgapID int64, msg *nas.Message) {
	fmt.Println("[gNB]   NAS Registration Accept received ✓")

	acc, err := nas.DecodeRegistrationAccept(msg.Payload)
	if err != nil {
		fmt.Printf("[gNB]   Failed to decode Registration Accept: %v\n", err)
		return
	}

	if acc.GUTI5G != nil {
		fmt.Printf("[gNB]   Assigned 5G-GUTI: PLMN=%s TMSI=0x%08X\n",
			acc.GUTI5G.PLMN, acc.GUTI5G.TMSI)
	}
	fmt.Printf("[gNB]   Allowed NSSAI: %d slice(s)\n", len(acc.AllowedNSSAI))

	// Send NAS Registration Complete back to AMF
	// Ref: TS 24.501 §8.2.9 / TS 23.502 §4.2.2.2.2 step 18
	nasComplete := nas.BuildRegistrationComplete()
	if err := g.sendUplinkNASTransport(conn, amfUeNgapID, ranUeNgapID, nasComplete); err != nil {
		fmt.Printf("[gNB]   Failed to send Registration Complete: %v\n", err)
		return
	}

	fmt.Println("[gNB]   NAS Registration Complete sent — UE registered ✓")

	// Phase 5: immediately request a PDU session after registration
	// Ref: TS 23.502 §4.3.2
	if err := g.startPDUSession(conn, amfUeNgapID, ranUeNgapID); err != nil {
		fmt.Printf("[gNB]   PDU session request failed: %v\n", err)
	}
}

// sendUplinkNASTransport wraps a NAS message in an NGAP UplinkNASTransport.
// Ref: TS 38.413 §9.2.5.3
func (g *GNB) sendUplinkNASTransport(conn net.Conn, amfUeNgapID, ranUeNgapID int64, nasPayload []byte) error {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeUplinkNASTransport
	pdu.InitiatingMessage.Criticality.Value = ngapType.CriticalityPresentIgnore
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentUplinkNASTransport

	ulNAS := ngapType.UplinkNASTransport{}

	// IE: AMF UE NGAP ID
	{
		ie := ngapType.UplinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDAMFUENGAPID
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.UplinkNASTransportIEsPresentAMFUENGAPID
		ie.Value.AMFUENGAPID = new(ngapType.AMFUENGAPID)
		ie.Value.AMFUENGAPID.Value = amfUeNgapID
		ulNAS.ProtocolIEs.List = append(ulNAS.ProtocolIEs.List, ie)
	}

	// IE: RAN UE NGAP ID
	{
		ie := ngapType.UplinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDRANUENGAPID
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.UplinkNASTransportIEsPresentRANUENGAPID
		ie.Value.RANUENGAPID = new(ngapType.RANUENGAPID)
		ie.Value.RANUENGAPID.Value = ranUeNgapID
		ulNAS.ProtocolIEs.List = append(ulNAS.ProtocolIEs.List, ie)
	}

	// IE: NAS-PDU
	{
		ie := ngapType.UplinkNASTransportIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDNASPDU
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.UplinkNASTransportIEsPresentNASPDU
		ie.Value.NASPDU = new(ngapType.NASPDU)
		ie.Value.NASPDU.Value = nasPayload
		ulNAS.ProtocolIEs.List = append(ulNAS.ProtocolIEs.List, ie)
	}

	pdu.InitiatingMessage.Value.UplinkNASTransport = &ulNAS

	data, err := ngapbuilder.EncodeNGAP(pdu)
	if err != nil {
		return fmt.Errorf("encode UplinkNASTransport: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("send UplinkNASTransport: %w", err)
	}

	fmt.Printf("[gNB]   UplinkNASTransport sent (%d bytes)\n", len(data))
	return nil
}

// startPDUSession sends a NAS PDU Session Establishment Request
// wrapped in a NAS UL NAS Transport, wrapped in NGAP UplinkNASTransport.
//
// Ref: TS 23.502 §4.3.2 / TS 24.501 §8.3.1
func (g *GNB) startPDUSession(conn net.Conn, amfUeNgapID, ranUeNgapID int64) error {
	fmt.Println("[gNB]   Initiating PDU Session Establishment for UE")

	// Build NAS SM PDU Session Establishment Request
	smReq := nas.BuildPDUSessionEstablishmentRequest(1, "internet")

	// Wrap in NAS MM UL NAS Transport container
	// Ref: TS 24.501 §8.2.14
	nasContainer := buildULNASTransportMM(1, smReq)

	// Send via NGAP UplinkNASTransport
	return g.sendUplinkNASTransport(conn, amfUeNgapID, ranUeNgapID, nasContainer)
}

// buildULNASTransportMM wraps an SM payload in a NAS MM UL NAS Transport.
//
// Byte layout (TS 24.501 §8.2.14):
//
//	[0]   EPD = 0x7E (5GS MM)
//	[1]   Security Header
//	[2]   Message Type = 0x67 (UL NAS Transport)
//	[3]   Payload container type = 0x01 (N1 SM info)
//	[4-5] Payload container length (2 bytes)
//	[6..] Payload container (SM message)
//	then: PDU Session ID IE
func buildULNASTransportMM(pduSessionID uint8, smPayload []byte) []byte {
	msg := []byte{
		nas.EPD5GSMobilityManagement,
		nas.SecurityHeaderTypePlain,
		0x67, // UL NAS Transport message type
		0x01, // Payload container type = N1 SM info
	}

	// Container length (2 bytes big-endian)
	msg = append(msg, byte(len(smPayload)>>8), byte(len(smPayload)))
	msg = append(msg, smPayload...)

	// PDU Session ID IE
	msg = append(msg, 0x12, pduSessionID)

	return msg
}

// handlePDUSessionAccept processes a DL NAS Transport carrying a PDU Session Accept.
// Extracts the UE IP and sets up the GTP-U user plane tunnel.
func (g *GNB) handlePDUSessionAccept(nasPayload []byte) {
	if len(nasPayload) < 6 {
		fmt.Println("[gNB]   DL NAS Transport payload too short")
		return
	}

	containerLen := int(nasPayload[4])<<8 | int(nasPayload[5])
	if 6+containerLen > len(nasPayload) {
		return
	}
	smPayload := nasPayload[6 : 6+containerLen]
	if len(smPayload) < 4 {
		return
	}

	smMsgType := smPayload[3]

	switch smMsgType {
	case nas.MsgTypePDUSessionEstablishmentAccept:
		fmt.Println("[gNB]   NAS PDU Session Establishment Accept received ✓")
		g.setupUserPlaneFromAccept(smPayload)

	case nas.MsgTypePDUSessionEstablishmentReject:
		fmt.Println("[gNB]   NAS PDU Session Establishment Reject received ✗")
		if len(smPayload) >= 5 {
			fmt.Printf("[gNB]   Reject cause: 0x%02X\n", smPayload[4])
		}
	}
}

// setupUserPlaneFromAccept parses the PDU Session Accept and sets up GTP-U.
func (g *GNB) setupUserPlaneFromAccept(smPayload []byte) {
	// Parse UE IP from PDU address IE (IEI=0x29)
	ueIP := ""
	upfAddr := "127.0.0.1:2152"
	var ulTEID uint32 = 1 // Default — real value comes from AMF via N2

	offset := 5 // skip: EPD(1) + PDUSessionID(1) + PTI(1) + MsgType(1) + SessionType(1)

	// Skip mandatory QoS Rules (length-prefixed, 2-byte length)
	if offset+2 <= len(smPayload) {
		qosLen := int(smPayload[offset])<<8 | int(smPayload[offset+1])
		offset += 2 + qosLen
	}
	// Skip Session AMBR (length-prefixed, 1-byte length)
	if offset+1 <= len(smPayload) {
		ambrLen := int(smPayload[offset])
		offset += 1 + ambrLen
	}

	// Parse optional IEs
	for offset < len(smPayload)-1 {
		iei := smPayload[offset]
		offset++
		if offset >= len(smPayload) {
			break
		}
		ieLen := int(smPayload[offset])
		offset++
		if offset+ieLen > len(smPayload) {
			break
		}
		ieData := smPayload[offset : offset+ieLen]
		offset += ieLen

		switch iei {
		case 0x29: // PDU Address
			if len(ieData) >= 5 && ieData[0] == 0x01 { // IPv4
				ueIP = fmt.Sprintf("%d.%d.%d.%d",
					ieData[1], ieData[2], ieData[3], ieData[4])
				fmt.Printf("[gNB]   UE allocated IP: %s\n", ueIP)
			}
		case 0x25: // DNN
			if len(ieData) > 1 {
				fmt.Printf("[gNB]   DNN: %s\n", string(ieData[1:]))
			}
		}
	}

	if ueIP == "" {
		fmt.Println("[gNB]   Could not parse UE IP from PDU Session Accept")
		ueIP = "10.0.0.1" // fallback for simulation
	}

	// g.pendingULTEID is set by the AMF N2 session resource setup
	// For now use the default value (1) — in Phase 6 this comes via NGAP
	// PDU Session Resource Setup Request (TS 38.413 §9.2.1.1)
	g.mu.RLock()
	if g.pendingULTEID != 0 {
		ulTEID = g.pendingULTEID
	}
	if g.pendingUPFAddr != "" {
		upfAddr = g.pendingUPFAddr
	}
	g.mu.RUnlock()

	up, err := g.SetupUserPlane(ueIP, ulTEID, upfAddr)
	if err != nil {
		fmt.Printf("[gNB]   Failed to setup user plane: %v\n", err)
		return
	}

	// Send a simulated ping to prove the user plane works
	time.Sleep(100 * time.Millisecond) // let UPF register session
	fmt.Println("[gNB]   Sending simulated ICMP ping through GTP tunnel...")
	if err := up.SendPing("8.8.8.8"); err != nil {
		fmt.Printf("[gNB]   Ping failed: %v\n", err)
	}
	up.WaitForReply(500 * time.Millisecond)
}
