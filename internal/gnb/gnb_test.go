// gnb_test.go — Tests for gNB context management and NGAP response handlers.
package gnb

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/free5gc/ngap"
	"github.com/free5gc/ngap/ngapType"

	ngapbuilder "github.com/afroash/5g-sim/internal/ngap"
)

// mockConn captures writes without a real network connection.
type mockConn struct {
	mu         sync.Mutex
	written    [][]byte
	remoteAddr net.Addr
}

func newMockConn() *mockConn {
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

func (m *mockConn) Read(b []byte) (int, error)        { return 0, nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return m.remoteAddr }
func (m *mockConn) LocalAddr() net.Addr                { return m.remoteAddr }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// --- Context Tests ---

func TestNewGNB(t *testing.T) {
	g := New(DefaultConfig())
	if g == nil {
		t.Fatal("New returned nil")
	}
	if g.IsSetup() {
		t.Error("IsSetup should be false before NG Setup")
	}
	if g.AMF() != nil {
		t.Error("AMF() should be nil before NG Setup")
	}
}

func TestWaitForSetup_Timeout(t *testing.T) {
	g := New(DefaultConfig())
	ok := g.WaitForSetup(50 * time.Millisecond)
	if ok {
		t.Error("WaitForSetup should return false on timeout")
	}
}

func TestSetAMFContext(t *testing.T) {
	g := New(DefaultConfig())

	amfCtx := &AMFContext{
		Name:         "test-amf",
		GUAMIRegion:  1,
		GUAMISet:     1,
		GUAMIPointer: 0,
		Capacity:     255,
		SetupAt:      time.Now(),
	}

	g.SetAMFContext(amfCtx)

	if !g.IsSetup() {
		t.Error("IsSetup should be true after SetAMFContext")
	}

	got := g.AMF()
	if got == nil {
		t.Fatal("AMF() returned nil after SetAMFContext")
	}
	if got.Name != "test-amf" {
		t.Errorf("AMF name = %q, want %q", got.Name, "test-amf")
	}

	// WaitForSetup should return immediately now
	ok := g.WaitForSetup(50 * time.Millisecond)
	if !ok {
		t.Error("WaitForSetup should return true after setup")
	}
}

// --- Handler Tests ---

// TestHandleNGSetupResponse builds a real NGSetupResponse, decodes it,
// and passes it to the gNB handler — verifying the AMF context is populated.
//
// Ref: TS 38.413 §9.2.6.2
func TestHandleNGSetupResponse(t *testing.T) {
	g := New(DefaultConfig())
	conn := newMockConn()

	// Build a real NGSetupResponse (same as what the AMF would send)
	respBytes, err := ngapbuilder.BuildNGSetupResponse(
		"test-amf", "00101", 1, 1, 0,
	)
	if err != nil {
		t.Fatalf("BuildNGSetupResponse: %v", err)
	}

	pdu, err := ngap.Decoder(respBytes)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	g.HandleNGSetupResponse(conn, pdu)

	if !g.IsSetup() {
		t.Fatal("gNB should be setup after HandleNGSetupResponse")
	}

	amfCtx := g.AMF()
	if amfCtx == nil {
		t.Fatal("AMF context should not be nil after setup")
	}
	if amfCtx.Name != "test-amf" {
		t.Errorf("AMF name = %q, want %q", amfCtx.Name, "test-amf")
	}
	if amfCtx.Capacity != 255 {
		t.Errorf("AMF capacity = %d, want 255", amfCtx.Capacity)
	}

	t.Logf("HandleNGSetupResponse: AMF=%s GUAMI=%d/%d/%d Capacity=%d ✓",
		amfCtx.Name, amfCtx.GUAMIRegion, amfCtx.GUAMISet, amfCtx.GUAMIPointer, amfCtx.Capacity)
}

// TestHandleNGSetupFailure verifies the failure handler doesn't panic
// and leaves the gNB in an unsetup state.
//
// Ref: TS 38.413 §9.2.6.3
func TestHandleNGSetupFailure(t *testing.T) {
	g := New(DefaultConfig())
	conn := newMockConn()

	failBytes, err := ngapbuilder.BuildNGSetupFailure(
		ngapType.CausePresentMisc,
		ngapType.CauseMiscPresentUnspecified,
	)
	if err != nil {
		t.Fatalf("BuildNGSetupFailure: %v", err)
	}

	pdu, err := ngap.Decoder(failBytes)
	if err != nil {
		t.Fatalf("Decoder: %v", err)
	}

	g.HandleNGSetupFailure(conn, pdu)

	if g.IsSetup() {
		t.Error("gNB should not be setup after NGSetupFailure")
	}

	t.Log("HandleNGSetupFailure: gNB remains unsetup ✓")
}

// TestHandlePDUSessionResourceSetupRequest verifies the gNB correctly processes
// a PDU Session Resource Setup Request from the AMF, stores the UPF tunnel info,
// and sends back a PDU Session Resource Setup Response with its DL F-TEID.
//
// Ref: TS 38.413 §9.2.1.1
func TestHandlePDUSessionResourceSetupRequest(t *testing.T) {
	g := New(DefaultConfig())
	conn := newMockConn()

	// First complete NG Setup so the gNB has an AMF context.
	respBytes, err := ngapbuilder.BuildNGSetupResponse("test-amf", "00101", 1, 1, 0)
	if err != nil {
		t.Fatalf("BuildNGSetupResponse: %v", err)
	}
	setupPDU, err := ngap.Decoder(respBytes)
	if err != nil {
		t.Fatalf("Decoder (setup): %v", err)
	}
	g.HandleNGSetupResponse(conn, setupPDU)

	// Build a PDUSessionResourceSetupRequest from the AMF carrying UPF tunnel info.
	reqBytes, err := ngapbuilder.BuildPDUSessionResourceSetupRequest(
		100,        // AMF-UE-NGAP-ID
		1,          // RAN-UE-NGAP-ID
		1,          // PDU Session ID
		"10.1.1.1", // UPF GTP-U address
		0xDEADBEEF, // UL TEID
	)
	if err != nil {
		t.Fatalf("BuildPDUSessionResourceSetupRequest: %v", err)
	}

	reqPDU, err := ngap.Decoder(reqBytes)
	if err != nil {
		t.Fatalf("Decoder (PDUSessionResourceSetupRequest): %v", err)
	}

	// Clear any writes from NG Setup
	conn.mu.Lock()
	conn.written = nil
	conn.mu.Unlock()

	g.HandlePDUSessionResourceSetupRequest(conn, reqPDU)

	// gNB should have sent a PDUSessionResourceSetupResponse
	resp := conn.LastWritten()
	if resp == nil {
		t.Fatal("gNB did not send a PDUSessionResourceSetupResponse")
	}

	respPDU, err := ngap.Decoder(resp)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if respPDU.Present != ngapType.NGAPPDUPresentSuccessfulOutcome {
		t.Errorf("response PDU present = %d, want SuccessfulOutcome (%d)",
			respPDU.Present, ngapType.NGAPPDUPresentSuccessfulOutcome)
	}
	if respPDU.SuccessfulOutcome.ProcedureCode.Value != ngapType.ProcedureCodePDUSessionResourceSetup {
		t.Errorf("response procedure code = %d, want PDUSessionResourceSetup (%d)",
			respPDU.SuccessfulOutcome.ProcedureCode.Value, ngapType.ProcedureCodePDUSessionResourceSetup)
	}

	// Decode response to verify it carries a valid DL F-TEID
	amfID, gnbAddr, dlTEID, err := ngapbuilder.DecodePDUSessionResourceSetupResponse(respPDU)
	if err != nil {
		t.Fatalf("DecodePDUSessionResourceSetupResponse: %v", err)
	}
	if amfID != 100 {
		t.Errorf("AMF-UE-NGAP-ID = %d, want 100", amfID)
	}
	if gnbAddr == "" {
		t.Error("gNB GTP address is empty")
	}
	if dlTEID == 0 {
		t.Error("DL TEID is 0")
	}

	t.Logf("PDUSessionResourceSetupRequest → Response: gNB=%s DL-TEID=0x%08X ✓",
		gnbAddr, dlTEID)
}

// TestDecodePLMN verifies the PLMN decode is the inverse of the encode
// in internal/ngap/builder.go.
func TestDecodePLMN(t *testing.T) {
	tests := []struct {
		input []byte
		want  string
	}{
		{[]byte{0x00, 0xF1, 0x10}, "00101"},  // MCC=001 MNC=01
		{[]byte{0x00, 0x01, 0x10}, "001001"}, // MCC=001 MNC=001
	}

	for _, tt := range tests {
		got := decodePLMN(tt.input)
		if got != tt.want {
			t.Errorf("decodePLMN(%x) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
