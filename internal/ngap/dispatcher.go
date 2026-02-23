// Package ngap provides NGAP message dispatching and building for the N2 interface.
//
// NGAP (Next Generation Application Protocol) is the protocol between the gNB
// and the AMF. It runs over SCTP (TS 38.412) and carries all N2 signalling:
// UE registration, session setup, handover, paging, etc.
//
// This package wraps github.com/free5gc/ngap to provide:
//   - A Dispatcher that routes decoded PDUs to registered handler functions
//   - A Builder that constructs well-formed outgoing NGAP messages
//
// Ref: TS 38.413 — NG Application Protocol (NGAP)
package ngap

import (
	"fmt"
	"net"

	"github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"
)

// HandlerFunc is called when a specific NGAP message is received.
// conn is the SCTP connection the message arrived on (used to send responses).
// pdu is the fully decoded NGAP PDU from free5gc/ngap.
type HandlerFunc func(conn net.Conn, pdu *ngapType.NGAPPDU)

// Dispatcher routes incoming NGAP PDUs to registered handler functions.
// Register a handler for each message type your NF needs to handle.
//
// Usage:
//
//	d := ngap.NewDispatcher()
//	d.Register(ngapType.ProcedureCodeNGSetup, ngapType.InitiatingMessage, handleNGSetup)
//	d.Dispatch(conn, rawBytes)
type Dispatcher struct {
	// handlers is keyed by "procedureCode:presentType"
	// e.g. "21:1" for NGSetup InitiatingMessage
	handlers map[string]HandlerFunc
}

// NewDispatcher creates a Dispatcher with no handlers registered.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]HandlerFunc),
	}
}

// Register associates a HandlerFunc with a specific NGAP procedure and direction.
//
// procedureCode: the NGAP procedure code (e.g. ngapType.ProcedureCodeNGSetup = 21)
// present:       which PDU type — use ngapType constants:
//
//	ngapType.NGAPPDUPresentInitiatingMessage    = 1
//	ngapType.NGAPPDUPresentSuccessfulOutcome    = 2
//	ngapType.NGAPPDUPresentUnsuccessfulOutcome  = 3
//
// Ref: TS 38.413 §9.1 — Message Functional Definition and Content
func (d *Dispatcher) Register(procedureCode int64, present int, fn HandlerFunc) {
	key := handlerKey(procedureCode, present)
	d.handlers[key] = fn
}

// Dispatch decodes raw SCTP bytes as an NGAP PDU and calls the registered
// handler for that procedure. If no handler is registered, it logs and drops.
//
// This is the function you pass to the SCTP layer's Handler callback.
func (d *Dispatcher) Dispatch(conn net.Conn, _ net.Addr, data []byte) {
	pdu, err := ngap.Decoder(data)
	if err != nil {
		fmt.Printf("[NGAP] Decode error: %v\n", err)
		return
	}

	procedureCode, present, err := extractProcedureCode(pdu)
	if err != nil {
		fmt.Printf("[NGAP] Could not extract procedure code: %v\n", err)
		return
	}

	key := handlerKey(procedureCode, present)
	fn, ok := d.handlers[key]
	if !ok {
		fmt.Printf("[NGAP] No handler registered for procedure %d (present=%d)\n",
			procedureCode, present)
		return
	}

	fmt.Printf("[NGAP] Dispatching procedure %d (present=%d)\n", procedureCode, present)
	fn(conn, pdu)
}

// handlerKey builds the map key from a procedure code and PDU present type.
func handlerKey(procedureCode int64, present int) string {
	return fmt.Sprintf("%d:%d", procedureCode, present)
}

// extractProcedureCode pulls the procedure code and present type from a decoded PDU.
// Every NGAP PDU is a CHOICE of InitiatingMessage | SuccessfulOutcome | UnsuccessfulOutcome,
// each of which carries a procedureCode identifying what operation this is.
//
// Ref: TS 38.413 §9.1
func extractProcedureCode(pdu *ngapType.NGAPPDU) (procedureCode int64, present int, err error) {
	switch pdu.Present {
	case ngapType.NGAPPDUPresentInitiatingMessage:
		if pdu.InitiatingMessage == nil {
			return 0, 0, fmt.Errorf("nil InitiatingMessage")
		}
		return pdu.InitiatingMessage.ProcedureCode.Value,
			ngapType.NGAPPDUPresentInitiatingMessage, nil

	case ngapType.NGAPPDUPresentSuccessfulOutcome:
		if pdu.SuccessfulOutcome == nil {
			return 0, 0, fmt.Errorf("nil SuccessfulOutcome")
		}
		return pdu.SuccessfulOutcome.ProcedureCode.Value,
			ngapType.NGAPPDUPresentSuccessfulOutcome, nil

	case ngapType.NGAPPDUPresentUnsuccessfulOutcome:
		if pdu.UnsuccessfulOutcome == nil {
			return 0, 0, fmt.Errorf("nil UnsuccessfulOutcome")
		}
		return pdu.UnsuccessfulOutcome.ProcedureCode.Value,
			ngapType.NGAPPDUPresentUnsuccessfulOutcome, nil

	default:
		return 0, 0, fmt.Errorf("unknown PDU present type: %d", pdu.Present)
	}
}

// Send encodes an NGAP PDU and writes it to the given connection.
// This is the single egress point for all outgoing NGAP messages.
func Send(conn net.Conn, pdu ngapType.NGAPPDU) error {
	data, err := ngap.Encoder(pdu)
	if err != nil {
		return fmt.Errorf("ngap encode: %w", err)
	}

	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("ngap send: %w", err)
	}

	fmt.Printf("[NGAP] Sent %d bytes\n", len(data))
	return nil
}
