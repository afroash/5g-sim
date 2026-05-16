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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ishidawataru/sctp"

	sctptransport "github.com/afroash/5g-sim/internal/sctp"
	"github.com/afroash/5g-sim/internal/nas"
	"github.com/afroash/5g-sim/internal/udm"
)

// Start connects to the gNB and drives UE registration and PDU session setup.
// Blocks until the session is established, then stays alive for manual testing.
// Ref: TS 23.502 §4.2.2, §4.3.2
func (u *UE) Start() error {

	fmt.Printf("[UE] Starting — SUPI: %s  gNB: %s:%d\n",
		u.config.SUPI, u.config.GNBAddress, u.config.GNBSCTPPort)

	if addr := strings.TrimSpace(u.config.UDMAddress); addr != "" {
		if _, err := udm.NewClient(addr).GetSubscription(u.config.SUPI); err != nil {
			return fmt.Errorf("ue: subscriber not provisioned in UDM: %w", err)
		}
		fmt.Printf("[UE] UDM subscriber check OK for %s\n", u.config.SUPI)
	}

	if err := u.connectToGNB(); err != nil {
		return fmt.Errorf("ue: connect to gNB: %w", err)
	}

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
	return u.receiveLoop()
}

// connectToGNB establishes an SCTP connection to the gNB's UE port with retries.
// Ref: TS 38.412 §5.1 (adapted)
func (u *UE) connectToGNB() error {
	const maxAttempts = 15
	const retryInterval = 5 * time.Second

	addr := &sctp.SCTPAddr{
		IPAddrs: []net.IPAddr{{IP: net.ParseIP(u.config.GNBAddress)}},
		Port:    u.config.GNBSCTPPort,
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
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
		time.Sleep(retryInterval)
	}
	return nil
}

// receiveLoop reads NAS messages from the gNB and drives the state machine.
func (u *UE) receiveLoop() error {
	buf := make([]byte, sctptransport.MaxMessageSize)
	sctpConn := u.conn.(*sctp.SCTPConn)

	for {
		n, _, err := sctpConn.SCTPRead(buf)
		if err != nil {
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
	u.allocatedIP = acc.AllocatedIP
	fmt.Printf("[UE] Allocated IP: %s  DNN: %s\n", u.allocatedIP, acc.DNN)

	if u.allocatedIP == "" {
		fmt.Println("[UE] WARNING: no IP in accept — skipping TUN setup")
		return nil
	}
	// note: we need to setup the TUN interface here, this is the interface that will be used to send and receive data to the UE.
	// while in clab this is a "real" int with a ip that is routable, if we are just running the ue as a standalone, 
	// this is a TUN interface will not come up.
	// TODO: we need to handle this case, and either use a "real" interface or a TUN interface.
	if err := u.setupTUN(u.allocatedIP); err != nil {
		fmt.Printf("[UE] TUN setup failed: %v\n", err)
		// Non-fatal — manual testing still possible via ping from host
	}
	// note: the connectivity test is used in clab only, in standalone we will not have a internet-sim to test against.
	// TODO: we need to handle this case, the ue at this point is allowed to send and receive data, we want to be able to
	// visually see requests going to and from ue towards upf and the data network over the N6. 
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
