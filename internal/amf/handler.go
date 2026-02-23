// handler.go — NGAP message handlers for the AMF.
//
// Each function here handles one specific NGAP procedure from a gNB.
// The Dispatcher in internal/ngap routes raw bytes to these functions.
//
// Current handlers:
//   - HandleNGSetupRequest  (TS 38.413 §9.2.6.1)
//
// Each handler follows the same pattern:
//  1. Extract IEs from the decoded PDU
//  2. Validate mandatory fields
//  3. Update AMF state (context.go)
//  4. Build and send the response
package amf

import (
	"fmt"
	"net"
	"time"

	"github.com/free5gc/aper"
	"github.com/free5gc/ngap/ngapType"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// HandleNGSetupRequest processes an NGSetupRequest from a gNB.
//
// The NG Setup procedure is the first thing a gNB does after the SCTP
// association is up. It tells the AMF who it is (GlobalRanNodeID),
// what areas it covers (SupportedTAList), and its capabilities.
// The AMF responds with its own identity (GUAMI) and supported slices.
//
// On success:  sends NGSetupResponse  → gNB is now "NG Setup Complete"
// On failure:  sends NGSetupFailure   → gNB must not send UE messages
//
// Ref: TS 38.413 §9.2.6.1
// Ref: TS 38.401 §8.7 — NG Setup procedure description
func (a *AMF) HandleNGSetupRequest(conn net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Printf("[AMF] Received NGSetupRequest from %s\n", conn.RemoteAddr())
	if a.Hub != nil {
		a.Hub.Separator("NG Setup [TS 38.413 §8.7.1]")
		a.Hub.Procedure(seqdiag.NodeGNB, seqdiag.NodeAMF, "NGSetupRequest", "TS 38.413 §9.2.6.1")
	}

	// Unpack the InitiatingMessage value — we know it's an NGSetupRequest
	// because the dispatcher only calls us for ProcedureCodeNGSetup + InitiatingMessage.
	req := pdu.InitiatingMessage.Value.NGSetupRequest
	if req == nil {
		fmt.Println("[AMF] NGSetupRequest is nil — dropping")
		a.sendSetupFailure(conn, ngapType.CausePresentProtocol,
			ngapType.CauseProtocolPresentAbstractSyntaxErrorReject)
		return
	}

	// --- Extract IEs ---
	// IEs arrive as a list — we iterate and pick out what we need.
	// Mandatory IEs: GlobalRANNodeID, SupportedTAList, DefaultPagingDRX
	// Optional IEs:  RANNodeName
	//
	// Ref: TS 38.413 Table 9.2.6.1-1

	var (
		globalRanNodeID string
		ranName         string
		supportedTACs   [][]byte
		foundRanNodeID  bool
		foundTAList     bool
	)

	for _, ie := range req.ProtocolIEs.List {
		switch ie.Id.Value {

		// IE: GlobalRANNodeID — mandatory
		// The unique identity of this gNB within the PLMN.
		// Ref: TS 38.413 §9.3.1.5
		case ngapType.ProtocolIEIDGlobalRANNodeID:
			foundRanNodeID = true
			ranNodeID := ie.Value.GlobalRANNodeID
			if ranNodeID == nil {
				continue
			}
			switch ranNodeID.Present {
			case ngapType.GlobalRANNodeIDPresentGlobalGNBID:
				if ranNodeID.GlobalGNBID != nil {
					globalRanNodeID = fmt.Sprintf("gNB-%x",
						ranNodeID.GlobalGNBID.GNBID.GNBID.Bytes)
				}
			default:
				globalRanNodeID = fmt.Sprintf("RAN-%d", ranNodeID.Present)
			}

		// IE: RANNodeName — optional
		// Human-readable name for this gNB. Nice to have for logging.
		case ngapType.ProtocolIEIDRANNodeName:
			if ie.Value.RANNodeName != nil {
				ranName = ie.Value.RANNodeName.Value
			}

		// IE: SupportedTAList — mandatory
		// The Tracking Areas this gNB covers. The AMF uses this to know
		// which gNB to page a UE through for a given TAC.
		// Ref: TS 38.413 §9.3.3.6
		case ngapType.ProtocolIEIDSupportedTAList:
			foundTAList = true
			if ie.Value.SupportedTAList == nil {
				continue
			}
			for _, taItem := range ie.Value.SupportedTAList.List {
				tac := make([]byte, len(taItem.TAC.Value))
				copy(tac, taItem.TAC.Value)
				supportedTACs = append(supportedTACs, tac)
				fmt.Printf("[AMF]   Supported TAC: %x\n", tac)
			}

		// IE: DefaultPagingDRX — mandatory but we just log it
		case ngapType.ProtocolIEIDDefaultPagingDRX:
			if ie.Value.DefaultPagingDRX != nil {
				fmt.Printf("[AMF]   DefaultPagingDRX: %d\n",
					ie.Value.DefaultPagingDRX.Value)
			}
		}
	}

	// --- Validate mandatory IEs ---
	if !foundRanNodeID {
		fmt.Println("[AMF] NGSetupRequest missing GlobalRANNodeID — rejecting")
		a.sendSetupFailure(conn, ngapType.CausePresentProtocol,
			ngapType.CauseProtocolPresentAbstractSyntaxErrorReject)
		return
	}
	if !foundTAList {
		fmt.Println("[AMF] NGSetupRequest missing SupportedTAList — rejecting")
		a.sendSetupFailure(conn, ngapType.CausePresentProtocol,
			ngapType.CauseProtocolPresentAbstractSyntaxErrorReject)
		return
	}

	// --- Build RAN context ---
	// Store what we know about this gNB so the AMF can use it later
	// (paging, handover, UE context lookups).
	ran := &RAN{
		Conn:            conn,
		GlobalRanNodeID: globalRanNodeID,
		Name:            ranName,
		SupportedTACs:   supportedTACs,
		ConnectedAt:     time.Now(),
	}
	a.AddRAN(conn, ran)

	fmt.Printf("[AMF] NG Setup accepted for %s\n", ran)

	// --- Send NGSetupResponse ---
	// Ref: TS 38.413 §9.2.6.2
	cfg := a.Config()
	data, err := ngapbuilder.BuildNGSetupResponse(
		cfg.Name,
		cfg.PLMN,
		cfg.RegionID,
		cfg.SetID,
		cfg.Pointer,
	)
	if err != nil {
		fmt.Printf("[AMF] Failed to build NGSetupResponse: %v\n", err)
		return
	}

	if err := a.sendNGAP(conn, data); err != nil {
		fmt.Printf("[AMF] Failed to send NGSetupResponse: %v\n", err)
		a.RemoveRAN(conn)
		return
	}

	fmt.Printf("[AMF] NGSetupResponse sent to %s ✓\n", ran)
	if a.Hub != nil {
		a.Hub.Procedure(seqdiag.NodeAMF, seqdiag.NodeGNB, "NGSetupResponse", "TS 38.413 §9.2.6.2")
	}
}

// sendSetupFailure is a helper that builds and sends an NGSetupFailure.
// Called when we reject a gNB's setup request.
//
// Ref: TS 38.413 §9.2.6.3
func (a *AMF) sendSetupFailure(conn net.Conn, causePresent int, causeValue aper.Enumerated) {
	data, err := ngapbuilder.BuildNGSetupFailure(causePresent, causeValue)
	if err != nil {
		fmt.Printf("[AMF] Failed to build NGSetupFailure: %v\n", err)
		return
	}
	if err := a.sendNGAP(conn, data); err != nil {
		fmt.Printf("[AMF] Failed to send NGSetupFailure: %v\n", err)
	}
}
