// builder.go — Constructs outgoing NGAP messages.
//
// Each function builds one specific NGAP message per the spec, populating
// all mandatory IEs (Information Elements). Optional IEs are added where noted.
//
// We start with the NG Setup procedure — the very first exchange between
// a gNB and an AMF after the SCTP association is established.
//
// Ref: TS 38.413 §9.2.6 — NG Setup
package ngap

import (
	"encoding/hex"
	"fmt"

	"github.com/free5gc/aper"
	"github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"
)

// BuildNGSetupRequest constructs the NGSetupRequest message sent by the gNB
// to the AMF when first connecting. It identifies the gNB and its supported
// slices/tracking areas.
//
// Parameters:
//   - gnbID:     The gNB's identity (28-bit value, e.g. 0x1234)
//   - tac:       Tracking Area Code (3 bytes, e.g. 0x000001)
//   - plmnID:    PLMN identifier — MCC+MNC encoded (e.g. "00101" for MCC=001 MNC=01)
//   - gnbName:   Human-readable name for this gNB
//
// Ref: TS 38.413 §9.2.6.1
func BuildNGSetupRequest(gnbID uint32, tac uint32, plmnID string, gnbName string) ([]byte, error) {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeNGSetup
	pdu.InitiatingMessage.Criticality.Value = ngapType.CriticalityPresentReject
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentNGSetupRequest

	req := ngapType.NGSetupRequest{}
	ie := ngapType.NGSetupRequestIEs{}

	// IE 1: GlobalRANNodeID — identifies this gNB to the AMF
	// Ref: TS 38.413 §9.3.1.5
	{
		item := ngapType.NGSetupRequestIEs{}
		item.Id.Value = ngapType.ProtocolIEIDGlobalRANNodeID
		item.Criticality.Value = ngapType.CriticalityPresentReject
		item.Value.Present = ngapType.NGSetupRequestIEsPresentGlobalRANNodeID

		globalRanNodeID := ngapType.GlobalRANNodeID{}
		globalRanNodeID.Present = ngapType.GlobalRANNodeIDPresentGlobalGNBID

		globalGNBID := ngapType.GlobalGNBID{}
		globalGNBID.PLMNIdentity = encodePLMN(plmnID)

		// gNB-ID is a BIT STRING of 22..32 bits — we use 28 bits (most common)
		// Ref: TS 38.413 §9.3.1.6
		gnbIDBits := aper.BitString{
			Bytes:     []byte{byte(gnbID >> 20), byte(gnbID >> 12), byte(gnbID >> 4), byte(gnbID << 4)},
			BitLength: 28,
		}
		globalGNBID.GNBID.Present = ngapType.GNBIDPresentGNBID
		globalGNBID.GNBID.GNBID = &gnbIDBits
		globalRanNodeID.GlobalGNBID = &globalGNBID
		item.Value.GlobalRANNodeID = &globalRanNodeID

		ie = item
		req.ProtocolIEs.List = append(req.ProtocolIEs.List, ie)
	}

	// IE 2: RANNodeName — optional human-readable gNB name
	// Ref: TS 38.413 §9.3.1.77 (optional IE, criticality=ignore)
	if gnbName != "" {
		item := ngapType.NGSetupRequestIEs{}
		item.Id.Value = ngapType.ProtocolIEIDRANNodeName
		item.Criticality.Value = ngapType.CriticalityPresentIgnore
		item.Value.Present = ngapType.NGSetupRequestIEsPresentRANNodeName
		item.Value.RANNodeName = new(ngapType.RANNodeName)
		item.Value.RANNodeName.Value = gnbName
		req.ProtocolIEs.List = append(req.ProtocolIEs.List, item)
	}

	// IE 3: SupportedTAList — the Tracking Areas this gNB serves
	// Ref: TS 38.413 §9.3.3.6
	{
		item := ngapType.NGSetupRequestIEs{}
		item.Id.Value = ngapType.ProtocolIEIDSupportedTAList
		item.Criticality.Value = ngapType.CriticalityPresentReject
		item.Value.Present = ngapType.NGSetupRequestIEsPresentSupportedTAList

		taItem := ngapType.SupportedTAItem{}

		// TAC is 3 bytes — encode the uint32 into the low 3 bytes
		tacBytes := []byte{byte(tac >> 16), byte(tac >> 8), byte(tac)}
		taItem.TAC.Value = tacBytes

		// Each TA item needs at least one BroadcastPLMNItem
		bcastItem := ngapType.BroadcastPLMNItem{}
		bcastItem.PLMNIdentity = encodePLMN(plmnID)

		// Add a default S-NSSAI (Single Network Slice Selection Assistance Info)
		// SST=1 (eMBB) with no SD — the most basic 5G slice
		// Ref: TS 23.501 §5.15
		snssai := ngapType.SNSSAI{}
		snssai.SST.Value = []byte{0x01} // SST=1: eMBB
		bcastItem.TAISliceSupportList.List = append(
			bcastItem.TAISliceSupportList.List,
			ngapType.SliceSupportItem{SNSSAI: snssai},
		)

		taItem.BroadcastPLMNList.List = append(taItem.BroadcastPLMNList.List, bcastItem)
		item.Value.SupportedTAList = &ngapType.SupportedTAList{}
		item.Value.SupportedTAList.List = append(item.Value.SupportedTAList.List, taItem)
		req.ProtocolIEs.List = append(req.ProtocolIEs.List, item)
	}

	// IE 4: DefaultPagingDRX — mandatory, how often UEs check for paging
	// Ref: TS 38.413 §9.3.1.23
	{
		item := ngapType.NGSetupRequestIEs{}
		item.Id.Value = ngapType.ProtocolIEIDDefaultPagingDRX
		item.Criticality.Value = ngapType.CriticalityPresentIgnore
		item.Value.Present = ngapType.NGSetupRequestIEsPresentDefaultPagingDRX
		item.Value.DefaultPagingDRX = new(ngapType.PagingDRX)
		// v128 = every 128 radio frames — a common default value
		item.Value.DefaultPagingDRX.Value = ngapType.PagingDRXPresentV128
		req.ProtocolIEs.List = append(req.ProtocolIEs.List, item)
	}

	pdu.InitiatingMessage.Value.NGSetupRequest = &req

	data, err := encodeNGAP(pdu)
	if err != nil {
		return nil, fmt.Errorf("BuildNGSetupRequest encode: %w", err)
	}

	fmt.Printf("[NGAP] Built NGSetupRequest (%d bytes): %s\n", len(data), hex.EncodeToString(data))
	return data, nil
}

// BuildNGSetupResponse constructs the NGSetupResponse sent by the AMF
// back to the gNB when the NG Setup succeeds.
//
// Parameters:
//   - amfName:   Human-readable AMF name
//   - plmnID:    PLMN the AMF serves
//   - amfRegion: AMF Region ID (part of the GUAMI)
//   - amfSet:    AMF Set ID (part of the GUAMI)
//   - amfPtr:    AMF Pointer (part of the GUAMI)
//
// Ref: TS 38.413 §9.2.6.2
func BuildNGSetupResponse(amfName, plmnID string, amfRegion, amfSet, amfPtr uint8) ([]byte, error) {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentSuccessfulOutcome
	pdu.SuccessfulOutcome = new(ngapType.SuccessfulOutcome)
	pdu.SuccessfulOutcome.ProcedureCode.Value = ngapType.ProcedureCodeNGSetup
	pdu.SuccessfulOutcome.Criticality.Value = ngapType.CriticalityPresentReject
	pdu.SuccessfulOutcome.Value.Present = ngapType.SuccessfulOutcomePresentNGSetupResponse

	resp := ngapType.NGSetupResponse{}

	// IE 1: AMFName
	// Ref: TS 38.413 §9.3.3.1
	{
		item := ngapType.NGSetupResponseIEs{}
		item.Id.Value = ngapType.ProtocolIEIDAMFName
		item.Criticality.Value = ngapType.CriticalityPresentReject
		item.Value.Present = ngapType.NGSetupResponseIEsPresentAMFName
		item.Value.AMFName = new(ngapType.AMFName)
		item.Value.AMFName.Value = amfName
		resp.ProtocolIEs.List = append(resp.ProtocolIEs.List, item)
	}

	// IE 2: ServedGUAMIList — the GUAMI(s) this AMF serves
	// GUAMI = Globally Unique AMF Identifier = PLMN + AMF Region + AMF Set + AMF Pointer
	// Ref: TS 38.413 §9.3.3.11, TS 23.003 §2.10
	{
		item := ngapType.NGSetupResponseIEs{}
		item.Id.Value = ngapType.ProtocolIEIDServedGUAMIList
		item.Criticality.Value = ngapType.CriticalityPresentReject
		item.Value.Present = ngapType.NGSetupResponseIEsPresentServedGUAMIList

		guamiItem := ngapType.ServedGUAMIItem{}
		guamiItem.GUAMI.PLMNIdentity = encodePLMN(plmnID)

		// AMF Region ID: 8 bits
		guamiItem.GUAMI.AMFRegionID.Value = aper.BitString{
			Bytes:     []byte{amfRegion},
			BitLength: 8,
		}
		// AMF Set ID: 10 bits
		guamiItem.GUAMI.AMFSetID.Value = aper.BitString{
			Bytes:     []byte{amfSet >> 2, (amfSet & 0x03) << 6},
			BitLength: 10,
		}
		// AMF Pointer: 6 bits
		guamiItem.GUAMI.AMFPointer.Value = aper.BitString{
			Bytes:     []byte{amfPtr << 2},
			BitLength: 6,
		}

		item.Value.ServedGUAMIList = new(ngapType.ServedGUAMIList)
		item.Value.ServedGUAMIList.List = append(item.Value.ServedGUAMIList.List, guamiItem)
		resp.ProtocolIEs.List = append(resp.ProtocolIEs.List, item)
	}

	// IE 3: RelativeAMFCapacity — used by gNB for AMF load balancing
	// Ref: TS 38.413 §9.3.3.13 (0-255, higher = more capacity)
	{
		item := ngapType.NGSetupResponseIEs{}
		item.Id.Value = ngapType.ProtocolIEIDRelativeAMFCapacity
		item.Criticality.Value = ngapType.CriticalityPresentIgnore
		item.Value.Present = ngapType.NGSetupResponseIEsPresentRelativeAMFCapacity
		item.Value.RelativeAMFCapacity = new(ngapType.RelativeAMFCapacity)
		item.Value.RelativeAMFCapacity.Value = 255 // max capacity
		resp.ProtocolIEs.List = append(resp.ProtocolIEs.List, item)
	}

	// IE 4: PLMNSupportList — slices the AMF supports per PLMN
	// Ref: TS 38.413 §9.3.3.19
	{
		item := ngapType.NGSetupResponseIEs{}
		item.Id.Value = ngapType.ProtocolIEIDPLMNSupportList
		item.Criticality.Value = ngapType.CriticalityPresentReject
		item.Value.Present = ngapType.NGSetupResponseIEsPresentPLMNSupportList

		plmnItem := ngapType.PLMNSupportItem{}
		plmnItem.PLMNIdentity = encodePLMN(plmnID)

		// Add SST=1 (eMBB) slice support — matches what gNB advertised
		snssai := ngapType.SNSSAI{}
		snssai.SST.Value = []byte{0x01}
		plmnItem.SliceSupportList.List = append(
			plmnItem.SliceSupportList.List,
			ngapType.SliceSupportItem{SNSSAI: snssai},
		)

		item.Value.PLMNSupportList = new(ngapType.PLMNSupportList)
		item.Value.PLMNSupportList.List = append(item.Value.PLMNSupportList.List, plmnItem)
		resp.ProtocolIEs.List = append(resp.ProtocolIEs.List, item)
	}

	pdu.SuccessfulOutcome.Value.NGSetupResponse = &resp

	data, err := encodeNGAP(pdu)
	if err != nil {
		return nil, fmt.Errorf("BuildNGSetupResponse encode: %w", err)
	}

	fmt.Printf("[NGAP] Built NGSetupResponse (%d bytes): %s\n", len(data), hex.EncodeToString(data))
	return data, nil
}

// BuildNGSetupFailure constructs the NGSetupFailure sent by the AMF
// when it rejects an NG Setup request.
//
// Ref: TS 38.413 §9.2.6.3
func BuildNGSetupFailure(causePresent int, causeValue aper.Enumerated) ([]byte, error) {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentUnsuccessfulOutcome
	pdu.UnsuccessfulOutcome = new(ngapType.UnsuccessfulOutcome)
	pdu.UnsuccessfulOutcome.ProcedureCode.Value = ngapType.ProcedureCodeNGSetup
	pdu.UnsuccessfulOutcome.Criticality.Value = ngapType.CriticalityPresentReject
	pdu.UnsuccessfulOutcome.Value.Present = ngapType.UnsuccessfulOutcomePresentNGSetupFailure

	fail := ngapType.NGSetupFailure{}

	// IE: Cause — why the setup failed
	// Ref: TS 38.413 §9.3.1.2
	{
		item := ngapType.NGSetupFailureIEs{}
		item.Id.Value = ngapType.ProtocolIEIDCause
		item.Criticality.Value = ngapType.CriticalityPresentIgnore
		item.Value.Present = ngapType.NGSetupFailureIEsPresentCause
		item.Value.Cause = new(ngapType.Cause)
		item.Value.Cause.Present = causePresent
		// Set cause value based on present type
		switch causePresent {
		case ngapType.CausePresentMisc:
			item.Value.Cause.Misc = new(ngapType.CauseMisc)
			item.Value.Cause.Misc.Value = causeValue
		case ngapType.CausePresentProtocol:
			item.Value.Cause.Protocol = new(ngapType.CauseProtocol)
			item.Value.Cause.Protocol.Value = causeValue
		}
		fail.ProtocolIEs.List = append(fail.ProtocolIEs.List, item)
	}

	pdu.UnsuccessfulOutcome.Value.NGSetupFailure = &fail

	data, err := encodeNGAP(pdu)
	if err != nil {
		return nil, fmt.Errorf("BuildNGSetupFailure encode: %w", err)
	}

	return data, nil
}

// encodePLMN encodes a PLMN ID string into the 3-byte BCD format used in 3GPP.
// Input format: 5-6 digit string, e.g. "00101" = MCC 001, MNC 01
//
//	or "001001" = MCC 001, MNC 001
//
// Encoding (TS 24.008 §10.5.1.13):
//
//	Byte 0: MCC digit 2 | MCC digit 1
//	Byte 1: MNC digit 3 | MCC digit 3   (0xF if MNC is 2 digits)
//	Byte 2: MNC digit 2 | MNC digit 1
func encodePLMN(plmn string) ngapType.PLMNIdentity {
	// Pad to 6 chars if 2-digit MNC
	if len(plmn) == 5 {
		plmn = plmn[:3] + "f" + plmn[3:]
	}

	d := func(i int) byte { return plmn[i] - '0' }
	f := func(i int) byte {
		if plmn[i] == 'f' || plmn[i] == 'F' {
			return 0xf
		}
		return plmn[i] - '0'
	}

	return ngapType.PLMNIdentity{
		Value: []byte{
			(d(1) << 4) | d(0), // MCC digit 2, MCC digit 1
			(f(3) << 4) | d(2), // MNC digit 3 (or F), MCC digit 3
			(d(5) << 4) | d(4), // MNC digit 2, MNC digit 1
		},
	}
}

// encodeNGAP is a thin wrapper around free5gc's Encoder for consistent error messages.
func encodeNGAP(pdu ngapType.NGAPPDU) ([]byte, error) {
	data, err := ngap.Encoder(pdu) //nolint - free5gc package
	if err != nil {
		return nil, fmt.Errorf("ngap.Encoder: %w", err)
	}
	return data, nil
}

// EncodeNGAP is the exported wrapper for use by other packages (amf, gnb).
func EncodeNGAP(pdu ngapType.NGAPPDU) ([]byte, error) {
	return encodeNGAP(pdu)
}
