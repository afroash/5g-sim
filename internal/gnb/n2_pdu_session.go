// n2_pdu_session.go — gNB handler for the N2 PDU Session Resource Setup procedure.
//
// The AMF sends a PDUSessionResourceSetupRequest carrying the UPF F-TEID.
// The gNB allocates a DL TEID for the gNB↔UPF path, stores the UPF tunnel
// info, and responds with its own GTP-U address and DL TEID.
//
// Ref: TS 38.413 §9.2.1
package gnb

import (
	"fmt"
	"net"

	"github.com/free5gc/aper"
	"github.com/free5gc/ngap/ngapType"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
)

// HandlePDUSessionResourceSetupRequest processes the AMF's request to set up
// N3 resources for a PDU session. Stores the UPF F-TEID and responds with
// the gNB's DL F-TEID.
// Ref: TS 38.413 §9.2.1.1
func (g *GNB) HandlePDUSessionResourceSetupRequest(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Println("[gNB] Received PDUSessionResourceSetupRequest from AMF")

	msg := pdu.InitiatingMessage.Value.PDUSessionResourceSetupRequest
	if msg == nil {
		fmt.Println("[gNB]   PDUSessionResourceSetupRequest is nil")
		return
	}

	var (
		amfUeNgapID int64
		ranUeNgapID int64
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
		case ngapType.ProtocolIEIDPDUSessionResourceSetupListSUReq:
			if ie.Value.PDUSessionResourceSetupListSUReq == nil ||
				len(ie.Value.PDUSessionResourceSetupListSUReq.List) == 0 {
				continue
			}
			item := ie.Value.PDUSessionResourceSetupListSUReq.List[0]

			var transfer ngapType.PDUSessionResourceSetupRequestTransfer
			if err := aper.Unmarshal(item.PDUSessionResourceSetupRequestTransfer, &transfer); err != nil {
				fmt.Printf("[gNB]   Failed to unmarshal transfer IE: %v\n", err)
				return
			}

			upfAddr, ulTEID, err := extractUPFFTEID(&transfer)
			if err != nil {
				fmt.Printf("[gNB]   Failed to extract UPF F-TEID: %v\n", err)
				return
			}

			g.mu.Lock()
			g.pendingULTEID = ulTEID
			g.pendingUPFAddr = fmt.Sprintf("%s:%d", upfAddr, g.config.UPFGTPPort)
			g.mu.Unlock()

			fmt.Printf("[gNB]   UPF F-TEID stored: addr=%s UL-TEID=0x%08X\n", upfAddr, ulTEID)
		}
	}

	dlTEID := g.allocateDLTEID()
	cfg := g.Config()

	// Persist the DL TEID so the user-plane setup uses the same value we
	// just announced to the AMF (and through it, to the UPF).
	g.mu.Lock()
	g.pendingDLTEID = dlTEID
	g.mu.Unlock()

	fmt.Printf("[gNB]   Allocated DL-TEID=0x%08X gNB-GTP=%s\n", dlTEID, cfg.GTPAddress)

	if err := g.sendPDUSessionResourceSetupResponse(conn, amfUeNgapID, ranUeNgapID, cfg.GTPAddress, dlTEID); err != nil {
		fmt.Printf("[gNB]   Failed to send PDUSessionResourceSetupResponse: %v\n", err)
		return
	}

	// Build the per-session user plane: opens the UPF-facing GTP-U socket,
	// registers the DL handler, and adds the session to g.ueSessions so the
	// UE-facing relay can forward uplink to the UPF.
	// Ref: TS 23.502 §4.3.2.2.2 — AN specific resource setup
	g.mu.RLock()
	ulTEID := g.pendingULTEID
	upfAddr := g.pendingUPFAddr
	g.mu.RUnlock()

	if ulTEID == 0 || upfAddr == "" {
		fmt.Printf("[gNB]   SetupUserPlane skipped — missing UL TEID or UPF addr (UL=0x%08X UPF=%q)\n",
			ulTEID, upfAddr)
		return
	}

	if _, err := g.SetupUserPlane(ranUeNgapID, ulTEID, upfAddr, dlTEID); err != nil {
		fmt.Printf("[gNB]   SetupUserPlane failed: %v\n", err)
	}
}

// extractUPFFTEID pulls the UPF GTP address and UL TEID from a request transfer IE.
// Ref: TS 38.413 §9.3.1.9 — UPTransportLayerInformation
func extractUPFFTEID(transfer *ngapType.PDUSessionResourceSetupRequestTransfer) (addr string, teid uint32, err error) {
	for _, ie := range transfer.ProtocolIEs.List {
		if ie.Id.Value != ngapType.ProtocolIEIDULNGUUPTNLInformation {
			continue
		}
		upTNL := ie.Value.ULNGUUPTNLInformation
		if upTNL == nil || upTNL.GTPTunnel == nil {
			return "", 0, fmt.Errorf("gnb: ULNGUUPTNLInformation has no GTP tunnel")
		}
		gtpTunnel := upTNL.GTPTunnel
		if len(gtpTunnel.TransportLayerAddress.Value.Bytes) < 4 {
			return "", 0, fmt.Errorf("gnb: TransportLayerAddress too short")
		}
		ipBytes := gtpTunnel.TransportLayerAddress.Value.Bytes[:4]
		addr = net.IP(ipBytes).String()

		if len(gtpTunnel.GTPTEID.Value) < 4 {
			return "", 0, fmt.Errorf("gnb: GTPTEID too short")
		}
		teid = uint32(gtpTunnel.GTPTEID.Value[0])<<24 |
			uint32(gtpTunnel.GTPTEID.Value[1])<<16 |
			uint32(gtpTunnel.GTPTEID.Value[2])<<8 |
			uint32(gtpTunnel.GTPTEID.Value[3])
		return addr, teid, nil
	}
	return "", 0, fmt.Errorf("gnb: ULNGUUPTNLInformation IE not found in transfer")
}

// sendPDUSessionResourceSetupResponse sends the gNB's DL F-TEID back to the AMF.
// Ref: TS 38.413 §9.2.1.2
func (g *GNB) sendPDUSessionResourceSetupResponse(
	conn net.Conn, amfUeNgapID, ranUeNgapID int64,
	gnbAddr string, dlTEID uint32,
) error {
	data, err := ngapbuilder.BuildPDUSessionResourceSetupResponse(
		amfUeNgapID, ranUeNgapID, gnbAddr, dlTEID,
	)
	if err != nil {
		return fmt.Errorf("gnb: build PDUSessionResourceSetupResponse: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("gnb: send PDUSessionResourceSetupResponse: %w", err)
	}
	fmt.Printf("[gNB]   PDUSessionResourceSetupResponse sent ✓\n")
	return nil
}
