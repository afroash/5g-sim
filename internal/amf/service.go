// service.go — Wires the AMF's SCTP server and NGAP dispatcher together.
//
// This is the entry point for the AMF process. It:
//  1. Creates the NGAP dispatcher and registers handlers
//  2. Starts the SCTP server on port 38412
//  3. Routes incoming bytes from gNBs through the dispatcher to handlers
//
// Think of this as the "main loop" of the AMF.
package amf

import (
	"fmt"
	"net"

	ngapdispatcher "github.com/afroash/5g-sim/internal/ngap"
	sctpserver "github.com/afroash/5g-sim/internal/sctp"
	"github.com/free5gc/ngap/ngapType"
)

// Start boots the AMF — registers NGAP handlers and begins listening
// for gNB connections on the configured SCTP port.
//
// This function blocks until the SCTP listener fails or is shut down.
func (a *AMF) Start() error {
	fmt.Printf("[AMF] Starting — Name: %s  PLMN: %s  GUAMI: %d/%d/%d\n",
		a.config.Name,
		a.config.PLMN,
		a.config.RegionID,
		a.config.SetID,
		a.config.Pointer,
	)

	// Build the NGAP dispatcher and register all handlers.
	d := ngapdispatcher.NewDispatcher()
	a.registerHandlers(d)

	// The SCTP server's Handler is the dispatcher's Dispatch method.
	// Every raw NGAP message received from any gNB flows through here.
	srv := sctpserver.NewServer(a.config.SCTPPort, func(conn net.Conn, addr net.Addr, data []byte) {
		d.Dispatch(conn, addr, data)
	})

	fmt.Printf("[AMF] Listening for gNB connections on SCTP port %d\n", a.config.SCTPPort)
	return srv.Listen()
}

// registerHandlers wires each NGAP procedure code to its AMF handler function.
// Add new handlers here as we implement more procedures.
//
// Procedure codes: TS 38.413 §9.1
func (a *AMF) registerHandlers(d *ngapdispatcher.Dispatcher) {
	// NG Setup — first procedure after SCTP association
	// Ref: TS 38.413 §9.2.6
	d.Register(
		ngapType.ProcedureCodeNGSetup,
		ngapType.NGAPPDUPresentInitiatingMessage,
		a.HandleNGSetupRequest,
	)

	// InitialUEMessage — first NAS message from a UE via gNB
	// Ref: TS 38.413 §9.2.5.1
	d.Register(
		ngapType.ProcedureCodeInitialUEMessage,
		ngapType.NGAPPDUPresentInitiatingMessage,
		a.HandleInitialUEMessage,
	)

	// UplinkNASTransport — subsequent UE→AMF NAS messages
	// Ref: TS 38.413 §9.2.5.3
	d.Register(
		ngapType.ProcedureCodeUplinkNASTransport,
		ngapType.NGAPPDUPresentInitiatingMessage,
		a.HandleUplinkNASTransport,
	)

	fmt.Println("[AMF] Registered NGAP handlers:")
	fmt.Println("[AMF]   ProcedureCodeNGSetup (InitiatingMessage)          → HandleNGSetupRequest")
	fmt.Println("[AMF]   ProcedureCodeInitialUEMessage (InitiatingMessage) → HandleInitialUEMessage")
	fmt.Println("[AMF]   ProcedureCodeUplinkNASTransport (InitiatingMessage) → HandleUplinkNASTransport")
}
