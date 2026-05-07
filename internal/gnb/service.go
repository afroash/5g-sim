// service.go — Connects the gNB to the AMF and drives the NG Setup procedure.
//
// Wires together:
//  1. SCTP client → connects to AMF on port 38412
//  2. NGAP dispatcher → routes AMF responses to gNB handlers
//  3. NGSetupRequest → sent immediately after SCTP is up
package gnb

import (
	"fmt"
	"net"
	"time"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
	sctpclient "github.com/afroash/5g-sim/internal/sctp"
	"github.com/free5gc/ngap/ngapType"
)

// Start connects the gNB to the AMF and initiates NG Setup.
//
// Flow:
//  1. Establish SCTP association to AMF
//  2. Register NGAP response handlers
//  3. Send NGSetupRequest
//  4. Block — the SCTP read loop runs in the background goroutine
//
// Ref: TS 38.412 §5.1 — gNB initiates SCTP association
// Ref: TS 38.413 §9.2.6.1 — NG Setup initiated by gNB
func (g *GNB) Start() error {
	cfg := g.Config()

	fmt.Printf("[gNB] Starting — ID: 0x%x  Name: %s  PLMN: %s  TAC: 0x%06x\n",
		cfg.GlobalGNBID, cfg.Name, cfg.PLMN, cfg.TAC)

	// Build dispatcher before connecting so it's ready when responses arrive.
	d := ngapbuilder.NewDispatcher()
	g.registerHandlers(d)

	// Connect to AMF over SCTP with retries — Server B may start before
	// Server A's AMF is ready.
	// Ref: TS 38.412 §5.1 — gNB initiates SCTP association
	const maxAttempts = 15
	const retryInterval = 5 * time.Second

	var client *sctpclient.Client
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var connErr error
		client, connErr = sctpclient.Connect(
			cfg.AMFAddress,
			cfg.AMFPort,
			func(conn net.Conn, addr net.Addr, data []byte) {
				d.Dispatch(conn, addr, data)
			},
		)
		if connErr == nil {
			break
		}
		if attempt == maxAttempts {
			return fmt.Errorf("connect to AMF at %s:%d after %d attempts: %w",
				cfg.AMFAddress, cfg.AMFPort, maxAttempts, connErr)
		}
		fmt.Printf("[gNB] AMF not reachable (%v), retry %d/%d in %s\n",
			connErr, attempt, maxAttempts, retryInterval)
		time.Sleep(retryInterval)
	}

	// Store the connection so Send() works.
	g.SetConn(client.Conn())

	fmt.Printf("[gNB] SCTP association established to AMF at %s:%d\n",
		cfg.AMFAddress, cfg.AMFPort)

	// Send NGSetupRequest — this is the first NGAP message after SCTP is up.
	// Ref: TS 38.413 §9.2.6.1
	if err := g.sendNGSetupRequest(); err != nil {
		return fmt.Errorf("NGSetupRequest: %w", err)
	}

	// Wait for NGSetupResponse with a 10 second timeout.
	fmt.Println("[gNB] Waiting for NGSetupResponse...")
	if !g.WaitForSetup(10 * time.Second) {
		return fmt.Errorf("NG Setup timed out after 10s — no response from AMF")
	}

	fmt.Println("[gNB] NG Setup complete — gNB is connected to 5G core ✓")

	// Bind the UE-facing GTP-U socket before the first UE shows up so it
	// can immediately push uplink data once its PDU session is established.
	// The UPF-facing tunnel is created per-session in SetupUserPlane.
	// Ref: TS 29.281 §4.4.2 — UDP/IP based transport
	if err := g.startUEGTPRelay(cfg.UEGTPPort); err != nil {
		return fmt.Errorf("gnb: relay: start UE-facing GTP-U: %w", err)
	}

	go func() {
		if err := g.startUEListener(); err != nil {
			fmt.Printf("[gNB] UE listener stopped: %v\n", err)
		}
	}()

	// Block — keep alive while SCTP read loop handles responses
	select {}
}

// registerHandlers wires AMF→gNB NGAP messages to their handler functions.
func (g *GNB) registerHandlers(d *ngapbuilder.Dispatcher) {
	// NGSetupResponse — AMF accepted our setup
	d.Register(
		ngapType.ProcedureCodeNGSetup,
		ngapType.NGAPPDUPresentSuccessfulOutcome,
		g.HandleNGSetupResponse,
	)

	// NGSetupFailure — AMF rejected our setup
	d.Register(
		ngapType.ProcedureCodeNGSetup,
		ngapType.NGAPPDUPresentUnsuccessfulOutcome,
		g.HandleNGSetupFailure,
	)

	// DownlinkNASTransport — NAS message from AMF destined for a UE
	// Ref: TS 38.413 §9.2.5.2
	d.Register(
		ngapType.ProcedureCodeDownlinkNASTransport,
		ngapType.NGAPPDUPresentInitiatingMessage,
		g.HandleDownlinkNASTransport,
	)

	// PDUSessionResourceSetup — AMF asks gNB to set up N3 resources
	// Ref: TS 38.413 §9.2.1.1
	d.Register(
		ngapType.ProcedureCodePDUSessionResourceSetup,
		ngapType.NGAPPDUPresentInitiatingMessage,
		g.HandlePDUSessionResourceSetupRequest,
	)

	fmt.Println("[gNB] Registered NGAP handlers:")
	fmt.Println("[gNB]   ProcedureCodeNGSetup (SuccessfulOutcome)        → HandleNGSetupResponse")
	fmt.Println("[gNB]   ProcedureCodeNGSetup (UnsuccessfulOutcome)      → HandleNGSetupFailure")
	fmt.Println("[gNB]   ProcedureCodeDownlinkNASTransport (Initiating)  → HandleDownlinkNASTransport")
	fmt.Println("[gNB]   ProcedureCodePDUSessionResourceSetup (Initiating) → HandlePDUSessionResourceSetupRequest")
}

// sendNGSetupRequest builds and sends the NGSetupRequest to the AMF.
// Ref: TS 38.413 §9.2.6.1
func (g *GNB) sendNGSetupRequest() error {
	cfg := g.Config()

	data, err := ngapbuilder.BuildNGSetupRequest(
		cfg.GlobalGNBID,
		cfg.TAC,
		cfg.PLMN,
		cfg.Name,
	)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	if err := g.Send(data); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	fmt.Printf("[gNB] NGSetupRequest sent (%d bytes)\n", len(data))
	return nil
}
