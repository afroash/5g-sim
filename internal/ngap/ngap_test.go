// ngap_test.go — Tests for NGAP dispatcher and message builders.
//
// Tests verify:
//  1. Messages encode without error
//  2. Encoded bytes decode back to the correct procedure code
//  3. The dispatcher routes to the correct handler
//
// Ref: TS 38.413
package ngap

import (
	"net"
	"testing"

	"github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"
)

// --- Builder Tests ---

// TestBuildNGSetupRequest verifies the NGSetupRequest encodes cleanly
// and decodes back to the correct procedure code.
// Ref: TS 38.413 §9.2.6.1
func TestBuildNGSetupRequest(t *testing.T) {
	data, err := BuildNGSetupRequest(
		0x1234,   // gNB ID (28-bit)
		0x000001, // TAC
		"00101",  // PLMN: MCC=001 MNC=01 (test network)
		"5g-sim-gnb-01",
	)
	if err != nil {
		t.Fatalf("BuildNGSetupRequest: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("encoded NGSetupRequest is empty")
	}

	// Decode it back and verify procedure code
	pdu, err := ngap.Decoder(data)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	if pdu.Present != ngapType.NGAPPDUPresentInitiatingMessage {
		t.Errorf("PDU present = %d, want InitiatingMessage (%d)",
			pdu.Present, ngapType.NGAPPDUPresentInitiatingMessage)
	}
	if pdu.InitiatingMessage.ProcedureCode.Value != ngapType.ProcedureCodeNGSetup {
		t.Errorf("ProcedureCode = %d, want NGSetup (%d)",
			pdu.InitiatingMessage.ProcedureCode.Value, ngapType.ProcedureCodeNGSetup)
	}

	t.Logf("NGSetupRequest: %d bytes, procedure code %d ✓",
		len(data), pdu.InitiatingMessage.ProcedureCode.Value)
}

// TestBuildNGSetupResponse verifies the NGSetupResponse encodes and decodes.
// Ref: TS 38.413 §9.2.6.2
func TestBuildNGSetupResponse(t *testing.T) {
	data, err := BuildNGSetupResponse(
		"5g-sim-amf",
		"00101",
		1, // AMF Region ID
		1, // AMF Set ID
		0, // AMF Pointer
	)
	if err != nil {
		t.Fatalf("BuildNGSetupResponse: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("encoded NGSetupResponse is empty")
	}

	pdu, err := ngap.Decoder(data)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	if pdu.Present != ngapType.NGAPPDUPresentSuccessfulOutcome {
		t.Errorf("PDU present = %d, want SuccessfulOutcome (%d)",
			pdu.Present, ngapType.NGAPPDUPresentSuccessfulOutcome)
	}
	if pdu.SuccessfulOutcome.ProcedureCode.Value != ngapType.ProcedureCodeNGSetup {
		t.Errorf("ProcedureCode = %d, want NGSetup (%d)",
			pdu.SuccessfulOutcome.ProcedureCode.Value, ngapType.ProcedureCodeNGSetup)
	}

	t.Logf("NGSetupResponse: %d bytes, procedure code %d ✓",
		len(data), pdu.SuccessfulOutcome.ProcedureCode.Value)
}

// TestBuildNGSetupFailure verifies the NGSetupFailure encodes and decodes.
// Ref: TS 38.413 §9.2.6.3
func TestBuildNGSetupFailure(t *testing.T) {
	data, err := BuildNGSetupFailure(
		ngapType.CausePresentMisc,
		ngapType.CauseMiscPresentUnspecified,
	)
	if err != nil {
		t.Fatalf("BuildNGSetupFailure: %v", err)
	}

	pdu, err := ngap.Decoder(data)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	if pdu.Present != ngapType.NGAPPDUPresentUnsuccessfulOutcome {
		t.Errorf("PDU present = %d, want UnsuccessfulOutcome (%d)",
			pdu.Present, ngapType.NGAPPDUPresentUnsuccessfulOutcome)
	}

	t.Logf("NGSetupFailure: %d bytes ✓", len(data))
}

// TestEncodePLMN verifies BCD PLMN encoding matches the 3GPP format.
// Ref: TS 24.008 §10.5.1.13
func TestEncodePLMN(t *testing.T) {
	tests := []struct {
		input   string
		wantHex [3]byte
		desc    string
	}{
		{
			input:   "00101",
			wantHex: [3]byte{0x00, 0xF1, 0x10},
			desc:    "MCC=001 MNC=01 (2-digit MNC, F padding)",
		},
		{
			input:   "001001",
			wantHex: [3]byte{0x00, 0x01, 0x10},
			desc:    "MCC=001 MNC=001 (3-digit MNC)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := encodePLMN(tt.input)
			if len(result.Value) != 3 {
				t.Fatalf("PLMN length = %d, want 3", len(result.Value))
			}
			for i, b := range tt.wantHex {
				if result.Value[i] != b {
					t.Errorf("byte[%d] = 0x%02X, want 0x%02X", i, result.Value[i], b)
				}
			}
		})
	}
}

// --- Dispatcher Tests ---

// TestDispatcherRouting verifies that the dispatcher calls the correct
// handler based on procedure code and PDU type.
func TestDispatcherRouting(t *testing.T) {
	called := false
	var calledProcedure int64

	d := NewDispatcher()
	d.Register(ngapType.ProcedureCodeNGSetup, ngapType.NGAPPDUPresentInitiatingMessage,
		func(conn net.Conn, pdu *ngapType.NGAPPDU) {
			called = true
			calledProcedure = pdu.InitiatingMessage.ProcedureCode.Value
		},
	)

	// Build a real NGSetupRequest and dispatch it
	data, err := BuildNGSetupRequest(0x1234, 0x000001, "00101", "test-gnb")
	if err != nil {
		t.Fatalf("BuildNGSetupRequest: %v", err)
	}

	// Use a mock conn — dispatcher only needs it to pass to the handler
	d.Dispatch(nil, nil, data)

	if !called {
		t.Fatal("handler was not called")
	}
	if calledProcedure != ngapType.ProcedureCodeNGSetup {
		t.Errorf("handler called with procedure %d, want %d",
			calledProcedure, ngapType.ProcedureCodeNGSetup)
	}

	t.Logf("Dispatcher routed NGSetup InitiatingMessage correctly ✓")
}

// TestDispatcherUnknownMessage verifies the dispatcher handles unregistered
// procedure codes gracefully (no panic, just a log).
func TestDispatcherUnknownMessage(t *testing.T) {
	d := NewDispatcher()
	// No handlers registered

	data, err := BuildNGSetupRequest(0x1234, 0x000001, "00101", "test-gnb")
	if err != nil {
		t.Fatalf("BuildNGSetupRequest: %v", err)
	}

	// Should not panic
	d.Dispatch(nil, nil, data)
	t.Log("Unregistered procedure handled gracefully ✓")
}
