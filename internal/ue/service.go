// service.go — UE connection and procedure state machine.
//
// Drives the full UE lifecycle after connecting to the gNB:
//  1. SCTP connect to gNB with retry
//  2. NAS Registration Request → Registration Accept → Registration Complete
//  3. NAS PDU Session Establishment Request → Establishment Accept
//  4. TUN interface setup (Part C)
//  5. Connectivity test (Part C)
//  6. Block — stay alive for manual testing
//
// Ref: TS 23.502 §4.2.2 — Registration, §4.3.2 — PDU Session
package ue

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ishidawataru/sctp"

	sctptransport "github.com/afroash/5g-sim/internal/sctp"
	"github.com/afroash/5g-sim/internal/nas"
	"github.com/afroash/5g-sim/internal/udm"
	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// Start connects to the gNB and drives UE registration and PDU session setup.
// Blocks until the session is established, then stays alive for manual testing.
// Ref: TS 23.502 §4.2.2, §4.3.2
func (u *UE) Start() error {
	return u.Run(context.Background())
}

// Run is like Start but respects ctx cancellation.
func (u *UE) Run(ctx context.Context) error {
	tag := u.config.SUPI
	if u.config.InstanceID != "" {
		tag = u.config.InstanceID + " " + tag
	}
	fmt.Printf("[UE] Starting — %s  gNB: %s:%d\n",
		tag, u.config.GNBAddress, u.config.GNBSCTPPort)

	u.setState(StateStarting)

	if addr := strings.TrimSpace(u.config.UDMAddress); addr != "" {
		if _, err := udm.NewClient(addr).GetSubscription(u.config.SUPI); err != nil {
			return fmt.Errorf("ue: subscriber not provisioned in UDM: %w", err)
		}
		fmt.Printf("[UE] UDM subscriber check OK for %s\n", u.config.SUPI)
	}

	if err := u.connectToGNB(ctx); err != nil {
		u.setState(StateFailed)
		if errors.Is(err, context.Canceled) {
			fmt.Printf("[UE] %s attach cancelled before/during gNB connect: %v\n", tag, err)
		}
		return fmt.Errorf("ue: connect to gNB: %w", err)
	}

	u.setState(StateRegistering)
	u.emitProcedure(seqdiag.NodeUE, seqdiag.NodeGNB,
		"RRC Setup Request", "Initial RRC connection", "TS 38.331 §5.3.3.3",
		map[string]string{"supi": u.config.SUPI})

	// Kick off registration.
	// Ref: TS 23.502 §4.2.2
	nasReq := nas.BuildRegistrationRequest(
		nas.SUPI(u.config.SUPI),
		nas.RegistrationTypeInitialRegistration,
		true,
	)
	if err := u.sendNAS(nasReq); err != nil {
		return fmt.Errorf("ue: send Registration Request: %w", err)
	}
	fmt.Printf("[UE] Registration Request sent (%d bytes)\n", len(nasReq))

	// Run the receive loop — drives the procedure state machine.
	return u.receiveLoop(ctx)
}

func (u *UE) emitProcedure(from, to seqdiag.Node, typ, detail, spec string, fields map[string]string) {
	if fields == nil {
		fields = make(map[string]string)
	}
	if u.config.InstanceID != "" {
		fields["ue_id"] = u.config.InstanceID
	}
	fields["supi"] = u.config.SUPI
	obspub.ProcedureWithDetail(from, to, typ, detail, spec, fields)
}

// connectToGNB establishes an SCTP connection to the gNB's UE port with retries.
// Ref: TS 38.412 §5.1 (adapted)
func (u *UE) connectToGNB(ctx context.Context) error {
	const maxAttempts = 15
	const retryInterval = 5 * time.Second

	addr := &sctp.SCTPAddr{
		IPAddrs: []net.IPAddr{{IP: net.ParseIP(u.config.GNBAddress)}},
		Port:    u.config.GNBSCTPPort,
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := sctp.DialSCTP("sctp", nil, addr)
		if err == nil {
			u.conn = conn
			fmt.Printf("[UE] Connected to gNB at %s:%d\n",
				u.config.GNBAddress, u.config.GNBSCTPPort)
			return nil
		}
		if attempt == maxAttempts {
			return fmt.Errorf("ue: gNB not reachable after %d attempts: %w", maxAttempts, err)
		}
		fmt.Printf("[UE] gNB not reachable (%v), retry %d/%d in %s\n",
			err, attempt, maxAttempts, retryInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
	return nil
}

// receiveLoop reads NAS messages from the gNB and drives the state machine.
func (u *UE) receiveLoop(ctx context.Context) error {
	buf := make([]byte, sctptransport.MaxMessageSize)
	sctpConn := u.conn.(*sctp.SCTPConn)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = sctpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := sctpConn.SCTPRead(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("ue: gNB connection closed: %w", err)
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])

		if err := u.handleNAS(payload); err != nil {
			fmt.Printf("[UE] NAS handling error: %v\n", err)
		}
	}
}

// handleNAS dispatches an incoming NAS message by type.
// Ref: TS 24.501 §9.7 — Message types
func (u *UE) handleNAS(data []byte) error {
	msg, err := nas.Decode(data)
	if err != nil {
		return fmt.Errorf("ue: NAS decode: %w", err)
	}

	switch msg.MessageType {
	case nas.MsgTypeRegistrationAccept:
		return u.handleRegistrationAccept(data, msg)
	case 0x68: // DL NAS Transport — carries SM container
		return u.handleDLNASTransport(data)
	default:
		fmt.Printf("[UE] Unhandled NAS type: 0x%02X\n", msg.MessageType)
	}
	return nil
}

// handleRegistrationAccept processes a NAS Registration Accept.
// Decodes the GUTI, sends Registration Complete, then triggers PDU session setup.
// Ref: TS 24.501 §8.2.7 / TS 23.502 §4.2.2.2.2 step 17
func (u *UE) handleRegistrationAccept(raw []byte, msg *nas.Message) error {
	fmt.Println("[UE] Registration Accept received ✓")

	acc, err := nas.DecodeRegistrationAccept(msg.Payload)
	if err != nil {
		return fmt.Errorf("ue: decode Registration Accept: %w", err)
	}
	if acc.GUTI5G != nil {
		fmt.Printf("[UE] 5G-GUTI assigned: PLMN=%s TMSI=0x%08X\n",
			acc.GUTI5G.PLMN, acc.GUTI5G.TMSI)
	}

	// Send Registration Complete.
	// Ref: TS 24.501 §8.2.9
	complete := nas.BuildRegistrationComplete()
	if err := u.sendNAS(complete); err != nil {
		return fmt.Errorf("ue: send Registration Complete: %w", err)
	}
	fmt.Println("[UE] Registration Complete sent ✓")

	// Immediately request a PDU session.
	// Ref: TS 23.502 §4.3.2
	smReq := nas.BuildPDUSessionEstablishmentRequest(1, u.config.DNN)
	ulTransport := nas.BuildULNASTransportMM(1, smReq)
	if err := u.sendNAS(ulTransport); err != nil {
		return fmt.Errorf("ue: send PDU Session Establishment Request: %w", err)
	}
	fmt.Printf("[UE] PDU Session Establishment Request sent (DNN=%s)\n", u.config.DNN)
	u.emitProcedure(seqdiag.NodeUE, seqdiag.NodeAMF,
		"NAS: PDU Session Request", "DNN: "+u.config.DNN+", SST:1", "TS 24.501 §8.2.26",
		map[string]string{"dnn": u.config.DNN})
	u.setState(StateRegistered)
	return nil
}

// handleDLNASTransport processes a NAS MM DL NAS Transport carrying an SM container.
// Ref: TS 24.501 §8.2.15
func (u *UE) handleDLNASTransport(data []byte) error {
	_, smPayload, err := nas.DecodeDLNASTransport(data)
	if err != nil {
		return fmt.Errorf("ue: decode DL NAS Transport: %w", err)
	}
	if len(smPayload) < 4 {
		return fmt.Errorf("ue: SM payload too short")
	}

	smMsgType := smPayload[3]
	switch smMsgType {
	case nas.MsgTypePDUSessionEstablishmentAccept:
		return u.handlePDUSessionAccept(smPayload)
	case nas.MsgTypePDUSessionEstablishmentReject:
		fmt.Println("[UE] PDU Session Establishment Reject ✗")
		if len(smPayload) >= 5 {
			fmt.Printf("[UE] Reject cause: 0x%02X\n", smPayload[4])
		}
	default:
		fmt.Printf("[UE] Unhandled SM type: 0x%02X\n", smMsgType)
	}
	return nil
}

// handlePDUSessionAccept processes a NAS PDU Session Establishment Accept.
// Extracts the allocated IP, then sets up the TUN interface and runs the connectivity test.
// Ref: TS 24.501 §8.3.2 / TS 23.502 §4.3.2.2.2 step 18
func (u *UE) handlePDUSessionAccept(smPayload []byte) error {
	fmt.Println("[UE] PDU Session Establishment Accept received ✓")

	acc, err := nas.DecodePDUSessionEstablishmentAccept(smPayload)
	if err != nil {
		return fmt.Errorf("ue: decode PDU Session Accept: %w", err)
	}
	// note: allocatedIP is given by SMF, if we are in clab, this is an ip that is routable,
	// if we are just running the ue as a standalone, this ip not routable.
	u.mu.Lock()
	u.allocatedIP = acc.AllocatedIP
	u.pduSessionID = acc.PDUSessionID
	if acc.DownlinkTEID != 0 {
		u.downlinkTEID = acc.DownlinkTEID
	}
	if acc.PDUSessionID != 0 {
		u.uplinkTEID = uint32(acc.PDUSessionID)
	}
	dlTEID := u.downlinkTEID
	ip := u.allocatedIP
	u.mu.Unlock()

	fmt.Printf("[UE] Allocated IP: %s  DNN: %s  DL-TEID=0x%08X\n", ip, acc.DNN, dlTEID)

	u.emitProcedure(seqdiag.NodeAMF, seqdiag.NodeUE,
		"NAS: PDU Session Accept", "UE IP: "+ip, "TS 24.501 §8.3.2",
		map[string]string{"ip": ip})

	if ip == "" {
		fmt.Println("[UE] WARNING: no IP in accept — skipping TUN setup")
		return nil
	}
	// note: we need to setup the TUN interface here, this is the interface that will be used to send and receive data to the UE.
	// while in clab this is a "real" int with a ip that is routable, if we are just running the ue as a standalone, 
	// this is a TUN interface will not come up.
	// TODO: we need to handle this case, and either use a "real" interface or a TUN interface.
	if err := u.setupTUN(ip); err != nil {
		fmt.Printf("[UE] TUN setup failed: %v\n", err)
		// Non-fatal — manual testing still possible via ping from host
	}
	u.setState(StatePDUActive)
	if u.onPDUActive != nil {
		u.onPDUActive(ip, dlTEID)
	}
	u.runConnectivityTest()
	return nil
}

// sendNAS writes a raw NAS message to the gNB SCTP connection.
func (u *UE) sendNAS(payload []byte) error {
	sctpConn := u.conn.(*sctp.SCTPConn)
	info := &sctp.SndRcvInfo{PPID: sctptransport.NGAPPPID, Stream: 0}
	_, err := sctpConn.SCTPWrite(payload, info)
	if err != nil {
		return fmt.Errorf("ue: SCTP write: %w", err)
	}
	return nil
}
