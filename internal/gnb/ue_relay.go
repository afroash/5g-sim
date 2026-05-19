// ue_relay.go — gNB SCTP listener for UE connections and NAS relay logic.
//
// In a real network, UEs connect via the radio interface. In this simulator,
// UE binaries connect via SCTP. The gNB accepts these connections and relays
// NAS messages transparently between UEs and the AMF:
//
//	UE → (raw NAS) → gNB → (NGAP UplinkNASTransport) → AMF
//	AMF → (NGAP DownlinkNASTransport) → gNB → (raw NAS) → UE
//
// Ref: TS 38.401 §8.3 — UE-associated logical NG-connection
package gnb

import (
	"fmt"
	"net"
	"sync/atomic"

	"github.com/ishidawataru/sctp"

	"github.com/afroash/5g-sim/internal/nas"
	sctptransport "github.com/afroash/5g-sim/internal/sctp"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// nextRanID is an atomic counter for RAN-UE-NGAP-ID allocation.
var nextRanID int64

// startUEListener starts the SCTP server for inbound UE connections.
// Each accepted UE gets a goroutine via handleUEConn.
// Ref: TS 38.412 §5.1 (adapted — UE→gNB direction)
func (g *GNB) startUEListener() error {
	cfg := g.Config()
	addr := &sctp.SCTPAddr{
		IPAddrs: []net.IPAddr{{IP: net.IPv4zero}},
		Port:    cfg.UEPort,
	}
	ln, err := sctp.ListenSCTP("sctp", addr)
	if err != nil {
		return fmt.Errorf("gnb: UE listener on port %d: %w", cfg.UEPort, err)
	}
	fmt.Printf("[gNB] UE listener started on SCTP port %d\n", cfg.UEPort)
	for {
		conn, err := ln.AcceptSCTP()
		if err != nil {
			return fmt.Errorf("gnb: accept UE connection: %w", err)
		}
		ranID := atomic.AddInt64(&nextRanID, 1)
		ctx := &UERelayContext{
			Conn:        conn,
			RanUeNgapID: ranID,
			FirstMsg:    true,
		}
		g.mu.Lock()
		g.uesByRanID[ranID] = ctx
		g.mu.Unlock()
		fmt.Printf("[gNB] UE connected — RAN-UE-NGAP-ID=%d from %s\n", ranID, conn.RemoteAddr())
		g.obsProcedure(seqdiag.NodeUE, seqdiag.NodeGNB,
			"RRC Setup Complete", "SCTP association established", "TS 38.331 §5.3.3.4", nil)
		go g.handleUEConn(ctx)
	}
}

// handleUEConn is the read loop for a single UE SCTP connection.
// It reads raw NAS messages and relays them toward the AMF via NGAP.
func (g *GNB) handleUEConn(ctx *UERelayContext) {
	defer func() {
		g.mu.Lock()
		delete(g.uesByRanID, ctx.RanUeNgapID)
		g.mu.Unlock()
		ctx.Conn.Close()
		fmt.Printf("[gNB] UE disconnected — RAN-UE-NGAP-ID=%d\n", ctx.RanUeNgapID)
	}()

	buf := make([]byte, sctptransport.MaxMessageSize)
	sctpConn := ctx.Conn.(*sctp.SCTPConn)

	for {
		n, _, err := sctpConn.SCTPRead(buf)
		if err != nil {
			fmt.Printf("[gNB] UE read error (RAN-UE-NGAP-ID=%d): %v\n", ctx.RanUeNgapID, err)
			return
		}
		nasPayload := make([]byte, n)
		copy(nasPayload, buf[:n])
		fmt.Printf("[gNB] UE→AMF: NAS %d bytes (RAN-UE-NGAP-ID=%d)\n", n, ctx.RanUeNgapID)
		g.relayUplinkNAS(ctx, nasPayload)
	}
}

// relayUplinkNAS wraps a NAS payload from a UE in NGAP and sends it to the AMF.
// Uses InitialUEMessage for the UE's first message, UplinkNASTransport for subsequent.
// Ref: TS 38.413 §9.2.5.1, §9.2.5.3
func (g *GNB) relayUplinkNAS(ctx *UERelayContext, nasPayload []byte) {
	g.mu.RLock()
	amfConn := g.conn
	amfUeNgapID := ctx.AMFUeNgapID
	g.mu.RUnlock()

	cfg := g.Config()
	var err error
	if ctx.FirstMsg {
		ctx.FirstMsg = false
		g.emitUplinkNASObs(nasPayload)
		err = g.sendInitialUEMessage(ctx.RanUeNgapID, nasPayload, cfg.TAC, cfg.PLMN)
	} else {
		err = g.sendUplinkNASTransport(amfConn, amfUeNgapID, ctx.RanUeNgapID, nasPayload)
	}
	if err != nil {
		fmt.Printf("[gNB] Failed to relay NAS to AMF (RAN-UE-NGAP-ID=%d): %v\n", ctx.RanUeNgapID, err)
	}
}

// relayDownlinkNAS forwards a NAS payload from the AMF to the correct UE connection.
// Called from HandleDownlinkNASTransport when a matching UE context exists.
// Ref: TS 38.413 §9.2.5.2
func (g *GNB) relayDownlinkNAS(ranUeNgapID, amfUeNgapID int64, nasPayload []byte) {
	g.mu.Lock()
	ctx, ok := g.uesByRanID[ranUeNgapID]
	if ok {
		ctx.AMFUeNgapID = amfUeNgapID
	}
	g.mu.Unlock()

	if !ok {
		fmt.Printf("[gNB] No UE context for RAN-UE-NGAP-ID=%d — dropping DL NAS\n", ranUeNgapID)
		return
	}

	sctpConn := ctx.Conn.(*sctp.SCTPConn)
	info := &sctp.SndRcvInfo{PPID: sctptransport.NGAPPPID, Stream: 0}
	if _, err := sctpConn.SCTPWrite(nasPayload, info); err != nil {
		fmt.Printf("[gNB] Failed to relay DL NAS to UE (RAN-UE-NGAP-ID=%d): %v\n",
			ranUeNgapID, err)
	} else {
		fmt.Printf("[gNB] AMF→UE: NAS %d bytes (RAN-UE-NGAP-ID=%d)\n",
			len(nasPayload), ranUeNgapID)
	}
	g.emitDownlinkNASObs(nasPayload)
}

func (g *GNB) emitUplinkNASObs(nasPayload []byte) {
	msg, err := nas.Decode(nasPayload)
	if err != nil {
		return
	}
	switch msg.MessageType {
	case nas.MsgTypeRegistrationRequest:
		g.obsProcedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"N2: Initial UE Message", "NAS Registration Request", "TS 38.413 §9.2.5.1", nil)
	case nas.MsgTypeRegistrationComplete:
		g.obsProcedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
			"N2: Uplink NAS Transport", "NAS Registration Complete", "TS 38.413 §9.2.5.3", nil)
	default:
		if msg.MessageType == 0x67 { // UL NAS Transport (PDU session, etc.)
			g.obsProcedure(seqdiag.NodeGNB, seqdiag.NodeAMF,
				"N2: Uplink NAS Transport", "NAS UL NAS Transport", "TS 38.413 §9.2.5.3", nil)
		}
	}
}

func (g *GNB) emitDownlinkNASObs(nasPayload []byte) {
	msg, err := nas.Decode(nasPayload)
	if err != nil {
		return
	}
	switch msg.MessageType {
	case nas.MsgTypeRegistrationAccept:
		g.obsProcedure(seqdiag.NodeAMF, seqdiag.NodeGNB,
			"N2: Downlink NAS Transport", "NAS Registration Accept", "TS 38.413 §9.2.5.2", nil)
		g.obsProcedure(seqdiag.NodeGNB, seqdiag.NodeUE,
			"RRC DL Information Transfer", "Deliver NAS Registration Accept", "TS 38.331 §5.3.3.5", nil)
	case 0x68: // DL NAS Transport
		g.obsProcedure(seqdiag.NodeAMF, seqdiag.NodeGNB,
			"N2: Downlink NAS Transport", "NAS DL NAS Transport (SM)", "TS 38.413 §9.2.5.2", nil)
		g.obsProcedure(seqdiag.NodeGNB, seqdiag.NodeUE,
			"RRC DL Information Transfer", "Deliver NAS SM container", "TS 38.331 §5.3.3.5", nil)
	}
}
