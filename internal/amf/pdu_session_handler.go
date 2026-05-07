// pdu_session_handler.go — AMF handler for PDU Session NAS messages.
//
// When a UE sends a PDU Session Establishment Request (NAS SM message),
// it arrives at the AMF inside an UL NAS Transport (NGAP). The AMF:
//  1. Extracts the NAS SM container
//  2. Calls the SMF via N11 (Nsmf_PDUSession_CreateSMContext)
//  3. Delivers the NAS SM response back to the UE via DL NAS Transport
//
// Ref: TS 23.502 §4.3.2 — PDU Session Establishment
// Ref: TS 29.502 — Nsmf_PDUSession service
package amf

import (
	"fmt"
	"net"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
	"github.com/afroash/5g-sim/internal/nas"
	"github.com/afroash/5g-sim/internal/smf"
	"github.com/afroash/5g-sim/pkg/seqdiag"
	"github.com/free5gc/ngap/ngapType"
)

// HandlePDUSessionEstablishmentRequest processes a NAS PDU Session
// Establishment Request delivered inside a NAS UL NAS Transport message.
//
// Called from routeNASMessage when MessageType == MsgTypePDUSessionEstablishmentRequest.
//
// Ref: TS 23.502 §4.3.2.2.1
func (a *AMF) HandlePDUSessionEstablishmentRequest(conn net.Conn, ue *UEContext, nasPayload []byte) {
	fmt.Println("[AMF]   Processing PDU Session Establishment Request")
	if a.Hub != nil {
		a.Hub.Separator("PDU Session Establishment [TS 23.502 §4.3.2]")
		a.Hub.Procedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"UplinkNASTransport (PDU Session Estab Request)", "TS 38.413 §9.2.5.3")
	}

	req, err := nas.DecodePDUSessionEstablishmentRequest(nasPayload)
	if err != nil {
		fmt.Printf("[AMF]   Failed to decode PDU Session Request: %v\n", err)
		return
	}

	fmt.Printf("[AMF]   PDU Session ID: %d  DNN: %s  Type: %d\n",
		req.PDUSessionID, req.RequestedDNN, req.PDUSessionType)

	dnn := req.RequestedDNN
	if dnn == "" {
		dnn = "internet"
	}

	// Step 1: Call SMF via N11 to create the session context
	// Ref: TS 23.502 §4.3.2.2.1 step 3
	smfClient := smf.NewClient(a.config.SMFAddress)

	smCtxReq := smf.SmContextCreateRequest{
		Supi:           ue.SUPI,
		PDUSessionID:   int(req.PDUSessionID),
		Dnn:            dnn,
		PDUSessionType: smf.PDUSessionTypeIPv4,
		ServingNfID:    "amf-sim-001",
		ServingNetwork: a.config.PLMN,
		SNssai: smf.SNssai{
			Sst: 1,
		},
	}

	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeAMF, seqdiag.NodeSMF,
			"Nsmf_PDUSession_CreateSMContext", "TS 29.502 §5.2.2.2",
			"supi", ue.SUPI, "dnn", dnn)
	}
	smCtxResp, err := smfClient.CreateSMContext(smCtxReq)
	if err != nil {
		fmt.Printf("[AMF]   SMF context creation failed: %v\n", err)
		// Send PDU Session Establishment Reject to UE
		nasReject := nas.BuildPDUSessionEstablishmentReject(
			req.PDUSessionID,
			0x1A, // Insufficient resources
		)
		a.sendPDUSessionNASToUE(conn, ue, req.PDUSessionID, nasReject)
		return
	}

	allocatedIP := ""
	if smCtxResp.PDUAddress != nil {
		allocatedIP = smCtxResp.PDUAddress.Ipv4Addr
	}

	fmt.Printf("[AMF]   SMF allocated IP: %s  ContextRef: %s\n",
		allocatedIP, smCtxResp.SmContextRef)

	if smCtxResp.GTPTunnel != nil {
		fmt.Printf("[AMF]   GTP tunnel: UL-TEID=0x%08X UPF=%s\n",
			smCtxResp.GTPTunnel.ULTEID, smCtxResp.GTPTunnel.UPFAddress)
		// In a full implementation we would send NGAP PDU Session Resource Setup Request
		// to the gNB here (TS 38.413 §9.2.1.1) with the UPF F-TEID.
		// For simulation we store it in the UE context — the gNB reads it back.
		ue.UPFAddr = smCtxResp.GTPTunnel.UPFAddress
		ue.ULTEID = smCtxResp.GTPTunnel.ULTEID
	}

	// Store the SM context reference in the UE context for later release
	ue.SMContextRef = smCtxResp.SmContextRef
	ue.AllocatedIP = allocatedIP

	// Step 2: Build NAS PDU Session Establishment Accept
	// Include GTP tunnel info so gNB can set up the user plane.
	// Ref: TS 23.502 §4.3.2.2.1 step 6
	var ulTEID uint32
	upfAddr := ""
	if smCtxResp.GTPTunnel != nil {
		ulTEID = smCtxResp.GTPTunnel.ULTEID
		upfAddr = smCtxResp.GTPTunnel.UPFAddress
	}
	nasAccept := nas.BuildPDUSessionEstablishmentAccept(
		req.PDUSessionID,
		allocatedIP,
		dnn,
	)

	ue.PendingNASAccept = nasAccept
	ue.PendingPDUSessionID = req.PDUSessionID
	if sendErr := a.sendPDUSessionResourceSetupRequest(conn, ue, ulTEID, upfAddr); sendErr != nil {
		fmt.Printf("[AMF]   N2 resource setup failed: %v — sending NAS accept directly\n", sendErr)
		ue.PendingNASAccept = nil
		a.sendPDUSessionNASToUE(conn, ue, req.PDUSessionID, nasAccept)
	}

	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeSMF, seqdiag.NodeAMF,
			"201 Created (IP allocated)", "TS 29.502 §6.1.6.3.2",
			"ip", allocatedIP)
		a.Hub.Procedure(seqdiag.NodeAMF, seqdiag.NodeGNB,
			"PDUSessionResourceSetupRequest", "TS 38.413 §9.2.1.1",
			"upf", upfAddr, "ul_teid", fmt.Sprintf("0x%08X", ulTEID))
	}
}

// sendPDUSessionResourceSetupRequest sends NGAP PDU Session Resource Setup Request
// to the gNB with the UPF F-TEID so it can set up its N3 GTP-U endpoint.
// Ref: TS 38.413 §9.2.1.1
func (a *AMF) sendPDUSessionResourceSetupRequest(
	conn net.Conn, ue *UEContext, ulTEID uint32, upfAddr string,
) error {
	data, err := ngapbuilder.BuildPDUSessionResourceSetupRequest(
		ue.AMFUeNgapID, 1, ue.PendingPDUSessionID, upfAddr, ulTEID,
	)
	if err != nil {
		return fmt.Errorf("amf: build PDUSessionResourceSetupRequest: %w", err)
	}
	if err := a.sendNGAP(conn, data); err != nil {
		return fmt.Errorf("amf: send PDUSessionResourceSetupRequest: %w", err)
	}
	fmt.Printf("[AMF]   PDUSessionResourceSetupRequest sent to gNB (UPF=%s UL-TEID=0x%08X)\n",
		upfAddr, ulTEID)
	return nil
}

// HandlePDUSessionResourceSetupResponse processes the gNB's N3 resource setup confirmation.
// Extracts the gNB DL F-TEID, then delivers the deferred NAS PDU Session Accept to the UE.
// Ref: TS 38.413 §9.2.1.2
func (a *AMF) HandlePDUSessionResourceSetupResponse(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Println("[AMF] Received PDUSessionResourceSetupResponse from gNB")

	amfUeNgapID, gnbAddr, dlTEID, err := ngapbuilder.DecodePDUSessionResourceSetupResponse(pdu)
	if err != nil {
		fmt.Printf("[AMF]   Decode failed: %v\n", err)
		return
	}

	fmt.Printf("[AMF]   gNB DL tunnel: addr=%s DL-TEID=0x%08X\n", gnbAddr, dlTEID)

	ue, ok := a.ues.GetByNgapID(amfUeNgapID)
	if !ok {
		fmt.Printf("[AMF]   No UE context for AMF-UE-NGAP-ID=%d\n", amfUeNgapID)
		return
	}

	nasAccept := ue.PendingNASAccept
	pduSessionID := ue.PendingPDUSessionID
	ue.PendingNASAccept = nil

	if nasAccept == nil {
		fmt.Println("[AMF]   No pending NAS Accept — ignoring")
		return
	}

	a.sendPDUSessionNASToUE(conn, ue, pduSessionID, nasAccept)
	fmt.Printf("[AMF]   PDU Session complete: UE=%s IP=%s gNB-DL-TEID=0x%08X ✓\n",
		ue.SUPI, ue.AllocatedIP, dlTEID)

	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"PDUSessionResourceSetupResponse", "TS 38.413 §9.2.1.2",
			"dl_teid", fmt.Sprintf("0x%08X", dlTEID))
		a.Hub.Procedure(seqdiag.NodeAMF, seqdiag.NodeGNB,
			"DownlinkNASTransport (PDU Session Estab Accept)", "TS 38.413 §9.2.5.2",
			"ip", ue.AllocatedIP)
	}
}

// sendPDUSessionNASToUE wraps a NAS SM message in a NAS MM UL NAS Transport
// container, then sends it via NGAP DownlinkNASTransport to the gNB.
//
// NAS SM messages cannot be sent standalone — they must be carried inside
// a NAS MM message (UL/DL NAS Transport) as an N1 SM container.
// Ref: TS 24.501 §8.2.15 — DL NAS Transport (MM message)
// Ref: TS 24.501 §9.11.3.29 — N1 SM container IE
func (a *AMF) sendPDUSessionNASToUE(conn net.Conn, ue *UEContext, pduSessionID uint8, smPayload []byte) {
	// Wrap SM message in NAS MM DL NAS Transport
	// Ref: TS 24.501 §8.2.15
	nasContainer := buildDLNASTransportMM(pduSessionID, smPayload)

	// Wrap in NGAP DownlinkNASTransport
	if err := a.sendDownlinkNASTransport(conn, ue.AMFUeNgapID, 1, nasContainer); err != nil {
		fmt.Printf("[AMF]   Failed to send PDU Session NAS to UE: %v\n", err)
	}
}

// buildDLNASTransportMM builds a NAS MM DL NAS Transport message
// that carries an SM container (PDU session message).
//
// Byte layout (TS 24.501 §8.2.15):
//
//	[0]   EPD = 0x7E
//	[1]   Security Header
//	[2]   Message Type = 0x68 (DL NAS Transport)
//	[3]   Payload container type = 0x01 (N1 SM info)
//	[4-5] Payload container length
//	[6..] Payload container (the SM message)
//	then: PDU Session ID IE
func buildDLNASTransportMM(pduSessionID uint8, smPayload []byte) []byte {
	msg := []byte{
		nas.EPD5GSMobilityManagement,
		nas.SecurityHeaderTypePlain,
		0x68, // DL NAS Transport message type
		0x01, // Payload container type = N1 SM info
	}

	// Payload container length (2 bytes big-endian)
	msg = append(msg, byte(len(smPayload)>>8), byte(len(smPayload)))
	msg = append(msg, smPayload...)

	// PDU Session ID IE (mandatory when type = N1 SM info)
	// Ref: TS 24.501 §9.11.3.41
	msg = append(msg,
		0x12, // IEI for PDU Session ID
		pduSessionID,
	)

	return msg
}
