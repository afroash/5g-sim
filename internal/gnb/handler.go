// handler.go — NGAP message handlers for the gNB.
//
// The gNB handles responses and requests that come FROM the AMF.
// Currently handles:
//   - NGSetupResponse    (TS 38.413 §9.2.6.2) — AMF accepts our setup
//   - NGSetupFailure     (TS 38.413 §9.2.6.3) — AMF rejects our setup
//
// More handlers will be added as we implement further procedures:
//   - DownlinkNASTransport  (AMF → gNB → UE NAS messages)
//   - InitialContextSetup   (AMF asks gNB to set up UE radio context)
//   - Paging                (AMF asks gNB to page a UE)
package gnb

import (
	"fmt"
	"net"
	"time"

	"github.com/free5gc/ngap/ngapType"

	"github.com/afroash/5g-sim/internal/nas"
)

// HandleNGSetupResponse processes the NGSetupResponse from the AMF.
//
// Extracts the AMF's identity (GUAMI), supported PLMNs, and capacity,
// stores them as AMFContext, and signals that the gNB is fully connected.
//
// Ref: TS 38.413 §9.2.6.2
func (g *GNB) HandleNGSetupResponse(_ net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Println("[gNB] Received NGSetupResponse from AMF")

	resp := pdu.SuccessfulOutcome.Value.NGSetupResponse
	if resp == nil {
		fmt.Println("[gNB] NGSetupResponse value is nil")
		return
	}

	amfCtx := &AMFContext{
		SetupAt: time.Now(),
	}

	// Iterate IEs and extract what we care about.
	// Ref: TS 38.413 Table 9.2.6.2-1
	for _, ie := range resp.ProtocolIEs.List {
		switch ie.Id.Value {

		// IE: AMFName — the AMF's human-readable name.
		// Ref: TS 38.413 §9.3.3.1
		case ngapType.ProtocolIEIDAMFName:
			if ie.Value.AMFName != nil {
				amfCtx.Name = ie.Value.AMFName.Value
				fmt.Printf("[gNB]   AMF Name: %s\n", amfCtx.Name)
			}

		// IE: ServedGUAMIList — the AMF's globally unique identifiers.
		// We take the first GUAMI (most deployments have one per AMF).
		// Ref: TS 38.413 §9.3.3.11 / TS 23.003 §2.10
		case ngapType.ProtocolIEIDServedGUAMIList:
			if ie.Value.ServedGUAMIList == nil || len(ie.Value.ServedGUAMIList.List) == 0 {
				continue
			}
			guami := ie.Value.ServedGUAMIList.List[0].GUAMI

			// Region ID: 8 bits
			if len(guami.AMFRegionID.Value.Bytes) > 0 {
				amfCtx.GUAMIRegion = guami.AMFRegionID.Value.Bytes[0]
			}
			// Set ID: 10 bits — high 8 in byte[0], low 2 in byte[1] bits 7-6
			if len(guami.AMFSetID.Value.Bytes) > 0 {
				amfCtx.GUAMISet = guami.AMFSetID.Value.Bytes[0]
			}
			// Pointer: 6 bits — stored in high 6 bits of byte[0]
			if len(guami.AMFPointer.Value.Bytes) > 0 {
				amfCtx.GUAMIPointer = guami.AMFPointer.Value.Bytes[0] >> 2
			}
			fmt.Printf("[gNB]   GUAMI: region=%d set=%d ptr=%d\n",
				amfCtx.GUAMIRegion, amfCtx.GUAMISet, amfCtx.GUAMIPointer)

		// IE: RelativeAMFCapacity — used for AMF load balancing.
		// Ref: TS 38.413 §9.3.3.13
		case ngapType.ProtocolIEIDRelativeAMFCapacity:
			if ie.Value.RelativeAMFCapacity != nil {
				amfCtx.Capacity = ie.Value.RelativeAMFCapacity.Value
				fmt.Printf("[gNB]   AMF Capacity: %d\n", amfCtx.Capacity)
			}

		// IE: PLMNSupportList — PLMNs and slices the AMF serves.
		// Ref: TS 38.413 §9.3.3.19
		case ngapType.ProtocolIEIDPLMNSupportList:
			if ie.Value.PLMNSupportList == nil {
				continue
			}
			for _, plmnItem := range ie.Value.PLMNSupportList.List {
				plmn := decodePLMN(plmnItem.PLMNIdentity.Value)
				amfCtx.PLMNs = append(amfCtx.PLMNs, plmn)
				fmt.Printf("[gNB]   Supported PLMN: %s\n", plmn)
			}
		}
	}

	// Store the AMF context and signal setup complete.
	g.SetAMFContext(amfCtx)
}

// HandleNGSetupFailure processes an NGSetupFailure from the AMF.
//
// Logs the cause and leaves the gNB in a disconnected state.
// In a real gNB this would trigger a retry with backoff.
//
// Ref: TS 38.413 §9.2.6.3
func (g *GNB) HandleNGSetupFailure(_ net.Conn, pdu *ngapType.NGAPPDU) {
	fmt.Println("[gNB] Received NGSetupFailure from AMF — setup rejected")

	fail := pdu.UnsuccessfulOutcome.Value.NGSetupFailure
	if fail == nil {
		fmt.Println("[gNB] NGSetupFailure value is nil")
		return
	}

	for _, ie := range fail.ProtocolIEs.List {
		switch ie.Id.Value {
		case ngapType.ProtocolIEIDCause:
			if ie.Value.Cause != nil {
				logCause(ie.Value.Cause)
			}
		case ngapType.ProtocolIEIDTimeToWait:
			// AMF can tell us how long to wait before retrying.
			// Ref: TS 38.413 §9.3.1.24
			if ie.Value.TimeToWait != nil {
				fmt.Printf("[gNB]   Time to wait before retry: %d\n",
					ie.Value.TimeToWait.Value)
			}
		}
	}
}

// HandleDownlinkNASTransport processes a NAS message from the AMF destined for a UE.
// If a UE relay context exists for the RAN-UE-NGAP-ID, the NAS payload is forwarded
// directly to the UE's SCTP connection. Otherwise it is decoded locally (legacy path
// for tests without a standalone UE binary).
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

	// Relay to standalone UE binary if one is connected.
	g.mu.RLock()
	_, hasUE := g.uesByRanID[ranUeNgapID]
	g.mu.RUnlock()

	if hasUE {
		g.relayDownlinkNAS(ranUeNgapID, amfUeNgapID, nasPayload)
		return
	}

	// Legacy path: no UE binary — decode NAS locally.
	nasMsg, err := nas.Decode(nasPayload)
	if err != nil {
		fmt.Printf("[gNB]   NAS decode error: %v\n", err)
		return
	}
	fmt.Printf("[gNB]   NAS message type: 0x%02X (no UE connected — dropping)\n", nasMsg.MessageType)
}

// decodePLMN reverses the BCD encoding back into a readable PLMN string.
// Input: 3-byte BCD encoded PLMN (TS 24.008 §10.5.1.13)
// Output: 5 or 6 digit string, e.g. "00101" or "001001"
func decodePLMN(b []byte) string {
	if len(b) < 3 {
		return "unknown"
	}
	mcc1 := b[0] & 0x0F
	mcc2 := (b[0] >> 4) & 0x0F
	mcc3 := b[1] & 0x0F
	mnc3 := (b[1] >> 4) & 0x0F
	mnc1 := b[2] & 0x0F
	mnc2 := (b[2] >> 4) & 0x0F

	if mnc3 == 0xF {
		// 2-digit MNC
		return fmt.Sprintf("%d%d%d%d%d", mcc1, mcc2, mcc3, mnc1, mnc2)
	}
	// 3-digit MNC
	return fmt.Sprintf("%d%d%d%d%d%d", mcc1, mcc2, mcc3, mnc3, mnc1, mnc2)
}

// logCause logs the NGAP cause IE in a readable form.
// Ref: TS 38.413 §9.3.1.2
func logCause(cause *ngapType.Cause) {
	switch cause.Present {
	case ngapType.CausePresentRadioNetwork:
		fmt.Printf("[gNB]   Cause (RadioNetwork): %d\n", cause.RadioNetwork.Value)
	case ngapType.CausePresentTransport:
		fmt.Printf("[gNB]   Cause (Transport): %d\n", cause.Transport.Value)
	case ngapType.CausePresentNas:
		fmt.Printf("[gNB]   Cause (NAS): %d\n", cause.Nas.Value)
	case ngapType.CausePresentProtocol:
		fmt.Printf("[gNB]   Cause (Protocol): %d\n", cause.Protocol.Value)
	case ngapType.CausePresentMisc:
		fmt.Printf("[gNB]   Cause (Misc): %d\n", cause.Misc.Value)
	default:
		fmt.Printf("[gNB]   Cause (unknown): %d\n", cause.Present)
	}
}
