// amf_test.go — Tests for AMF context management and NGAP handlers.
//
// These tests use a mock net.Conn to capture outgoing NGAP bytes
// without needing a real SCTP connection.
package amf

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
)

// mockConn is a net.Conn that captures written bytes for inspection.
type mockConn struct {
	mu         sync.Mutex
	written    [][]byte
	remoteAddr net.Addr
}

func newMockConn(_ string) *mockConn {
	return &mockConn{
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 38412},
	}
}

func (m *mockConn) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	m.written = append(m.written, cp)
	return len(b), nil
}

func (m *mockConn) LastWritten() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.written) == 0 {
		return nil
	}
	return m.written[len(m.written)-1]
}

func (m *mockConn) Read(b []byte) (int, error)         { return 0, nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return m.remoteAddr }
func (m *mockConn) LocalAddr() net.Addr                { return m.remoteAddr }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// --- Context Tests ---

func TestNewAMF(t *testing.T) {
	a := New(DefaultConfig())
	if a == nil {
		t.Fatal("New returned nil")
	}
	if a.RANCount() != 0 {
		t.Errorf("initial RAN count = %d, want 0", a.RANCount())
	}
}

func TestAddRemoveRAN(t *testing.T) {
	a := New(DefaultConfig())
	conn := newMockConn("127.0.0.1:40000")

	ran := &RAN{
		Conn:            conn,
		GlobalRanNodeID: "gNB-001",
		Name:            "test-gnb",
		ConnectedAt:     time.Now(),
	}

	// Add
	a.AddRAN(conn, ran)
	if a.RANCount() != 1 {
		t.Errorf("RAN count after add = %d, want 1", a.RANCount())
	}

	// Get
	got, ok := a.GetRAN(conn)
	if !ok {
		t.Fatal("GetRAN returned false after AddRAN")
	}
	if got.GlobalRanNodeID != ran.GlobalRanNodeID {
		t.Errorf("got RAN ID %q, want %q", got.GlobalRanNodeID, ran.GlobalRanNodeID)
	}

	// Remove
	a.RemoveRAN(conn)
	if a.RANCount() != 0 {
		t.Errorf("RAN count after remove = %d, want 0", a.RANCount())
	}
}

// --- Handler Tests ---

// TestHandleNGSetupRequest sends a real encoded NGSetupRequest to the AMF
// handler and verifies the response is a valid NGSetupResponse.
//
// Ref: TS 38.413 §9.2.6.1 / §9.2.6.2
func TestHandleNGSetupRequest(t *testing.T) {
	a := New(DefaultConfig())
	conn := newMockConn("127.0.0.1:40000")

	// Build a real NGSetupRequest
	reqBytes, err := ngapbuilder.BuildNGSetupRequest(0x1234, 0x000001, "00101", "test-gnb")
	if err != nil {
		t.Fatalf("BuildNGSetupRequest: %v", err)
	}

	// Decode it into a PDU (simulating what the dispatcher does)
	pdu, err := ngap.Decoder(reqBytes)
	if err != nil {
		t.Fatalf("ngap.Decoder: %v", err)
	}

	// Call the handler directly
	a.HandleNGSetupRequest(conn, pdu)

	// Handler should have written an NGSetupResponse
	resp := conn.LastWritten()
	if resp == nil {
		t.Fatal("handler did not send a response")
	}

	// Decode and verify it's an NGSetupResponse
	respPDU, err := ngap.Decoder(resp)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if respPDU.Present != ngapType.NGAPPDUPresentSuccessfulOutcome {
		t.Errorf("response PDU present = %d, want SuccessfulOutcome (%d)",
			respPDU.Present, ngapType.NGAPPDUPresentSuccessfulOutcome)
	}
	if respPDU.SuccessfulOutcome.ProcedureCode.Value != ngapType.ProcedureCodeNGSetup {
		t.Errorf("response procedure code = %d, want NGSetup (%d)",
			respPDU.SuccessfulOutcome.ProcedureCode.Value, ngapType.ProcedureCodeNGSetup)
	}

	// gNB should now be registered
	if a.RANCount() != 1 {
		t.Errorf("RAN count after setup = %d, want 1", a.RANCount())
	}

	t.Logf("NGSetupRequest → NGSetupResponse: %d bytes ✓", len(resp))
	t.Logf("Connected gNBs: %d ✓", a.RANCount())
}

// TestHandleNGSetupRequest_MissingIEs verifies the AMF sends NGSetupFailure
// when a malformed request arrives with no IEs.
func TestHandleNGSetupRequest_MissingIEs(t *testing.T) {
	a := New(DefaultConfig())
	conn := newMockConn("127.0.0.1:40001")

	// Craft a PDU with an empty NGSetupRequest (no IEs)
	pdu := &ngapType.NGAPPDU{}
	pdu.Present = ngapType.NGAPPDUPresentInitiatingMessage
	pdu.InitiatingMessage = new(ngapType.InitiatingMessage)
	pdu.InitiatingMessage.ProcedureCode.Value = ngapType.ProcedureCodeNGSetup
	pdu.InitiatingMessage.Value.Present = ngapType.InitiatingMessagePresentNGSetupRequest
	pdu.InitiatingMessage.Value.NGSetupRequest = &ngapType.NGSetupRequest{}
	// No IEs — mandatory fields missing

	a.HandleNGSetupRequest(conn, pdu)

	resp := conn.LastWritten()
	if resp == nil {
		t.Fatal("handler did not send any response")
	}

	respPDU, err := ngap.Decoder(resp)
	if err != nil {
		t.Fatalf("decode failure response: %v", err)
	}

	if respPDU.Present != ngapType.NGAPPDUPresentUnsuccessfulOutcome {
		t.Errorf("response PDU present = %d, want UnsuccessfulOutcome (%d)",
			respPDU.Present, ngapType.NGAPPDUPresentUnsuccessfulOutcome)
	}

	// gNB should NOT be registered after a failed setup
	if a.RANCount() != 0 {
		t.Errorf("RAN count after failed setup = %d, want 0", a.RANCount())
	}

	t.Log("Missing IEs → NGSetupFailure ✓")
}

// TestHandlePDUSessionResourceSetupResponse verifies that when the gNB sends
// back a PDU Session Resource Setup Response, the AMF extracts the DL F-TEID
// and delivers the deferred NAS PDU Session Establishment Accept to the UE.
//
// Ref: TS 38.413 §9.2.1.2
func TestHandlePDUSessionResourceSetupResponse(t *testing.T) {
	a := New(DefaultConfig())
	conn := newMockConn("127.0.0.1:40000")

	// Pre-populate a UE context with a pending NAS Accept, simulating the state
	// after HandlePDUSessionEstablishmentRequest ran.
	ue := &UEContext{
		SUPI:                "imsi-001010000000001",
		AllocatedIP:         "10.45.0.2",
		PendingNASAccept:    []byte{0x7E, 0x00, 0xC2, 0x01, 0x00, 0x0A}, // minimal NAS SM
		PendingPDUSessionID: 1,
	}
	a.ues.Add(ue) // assigns AMFUeNgapID = 1

	// Build a PDUSessionResourceSetupResponse from the gNB (DL F-TEID)
	respBytes, err := ngapbuilder.BuildPDUSessionResourceSetupResponse(
		ue.AMFUeNgapID, // AMF-UE-NGAP-ID
		1,              // RAN-UE-NGAP-ID
		"10.1.1.1",     // gNB GTP-U address
		0xCAFEBABE,     // DL TEID
	)
	if err != nil {
		t.Fatalf("BuildPDUSessionResourceSetupResponse: %v", err)
	}

	pdu, err := ngap.Decoder(respBytes)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	a.HandlePDUSessionResourceSetupResponse(conn, pdu)

	// AMF should have sent the deferred NAS PDU Session Establishment Accept
	written := conn.LastWritten()
	if written == nil {
		t.Fatal("AMF did not send NAS PDU Session Accept after receiving gNB response")
	}

	// The pending accept should be cleared
	if ue.PendingNASAccept != nil {
		t.Error("PendingNASAccept should be nil after response is delivered")
	}

	t.Logf("HandlePDUSessionResourceSetupResponse: NAS Accept delivered (%d bytes) ✓", len(written))
}
