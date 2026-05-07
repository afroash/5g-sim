// ue_simulator.go — Legacy NGAP helpers used by the gNB UE relay.
//
// sendInitialUEMessage and sendUplinkNASTransport are called from ue_relay.go
// when relaying UE NAS messages toward the AMF.
//
// Ref: TS 38.413 §9.2.5.1 — Initial UE Message
// Ref: TS 38.413 §9.2.5.3 — Uplink NAS Transport
package gnb

import (
	"fmt"
	"net"

	"github.com/free5gc/ngap/ngapType"

	"github.com/afroash/5g-sim/internal/nas"
	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
)

// SimulatedUE represents a UE being served by this gNB.
type SimulatedUE struct {
	SUPI        nas.SUPI
	RanUeNgapID int64
	GUTI        *nas.GUTI5G
}

// sendInitialUEMessage wraps a NAS payload in an NGAP InitialUEMessage and sends it to the AMF.
// Called for the first NAS message from a newly connected UE.
// Ref: TS 38.413 §9.2.5.1
func (g *GNB) sendInitialUEMessage(ranUeNgapID int64, nasPayload []byte, tac uint32, plmn string) error {
	pdu := ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeInitialUEMessage
	pdu.InitiatingMessage.Criticality.Value = ngapType.CriticalityPresentIgnore
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentInitialUEMessage

	initUE := ngapType.InitialUEMessage{}

	// IE: RAN UE NGAP ID
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

	// IE: NAS-PDU
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

	// IE: UserLocationInformation
	// Ref: TS 38.413 §9.3.1.15
	{
		ie := ngapType.InitialUEMessageIEs{}
		ie.Id.Value = ngapType.ProtocolIEIDUserLocationInformation
		ie.Criticality.Value = ngapType.CriticalityPresentReject
		ie.Value.Present = ngapType.InitialUEMessageIEsPresentUserLocationInformation

		locInfo := ngapType.UserLocationInformation{}
		locInfo.Present = ngapType.UserLocationInformationPresentUserLocationInformationNR

		nrInfo := ngapType.UserLocationInformationNR{}

		tacBytes := []byte{byte(tac >> 16), byte(tac >> 8), byte(tac)}
		nrInfo.TAI.TAC.Value = tacBytes
		nrInfo.TAI.PLMNIdentity.Value = encodePLMNBytes(plmn)

		// Ref: TS 38.413 §9.3.1.7
		nrInfo.NRCGI.PLMNIdentity.Value = encodePLMNBytes(plmn)
		nrInfo.NRCGI.NRCellIdentity.Value.Bytes = []byte{0x00, 0x00, 0x00, 0x00, 0x10}
		nrInfo.NRCGI.NRCellIdentity.Value.BitLength = 36

		locInfo.UserLocationInformationNR = &nrInfo
		ie.Value.UserLocationInformation = &locInfo
		initUE.ProtocolIEs.List = append(initUE.ProtocolIEs.List, ie)
	}

	// IE: RRCEstablishmentCause
	// Ref: TS 38.413 §9.3.1.10
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

// sendUplinkNASTransport wraps a NAS message in an NGAP UplinkNASTransport and sends it.
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
