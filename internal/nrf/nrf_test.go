// nrf_test.go — Tests for the NRF registry and HTTP API.
//
// Uses httptest.NewServer so no real port is needed.
package nrf

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Registry Tests ---

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	profile := &NFProfile{
		NfInstanceID:  "amf-001",
		NfType:        NFTypeAMF,
		NfStatus:      NFStatusRegistered,
		PlmnList:      []string{"00101"},
		IPv4Addresses: []string{"127.0.0.1"},
	}

	isNew := r.Register(profile)
	if !isNew {
		t.Error("first registration should return isNew=true")
	}

	got, ok := r.Get("amf-001")
	if !ok {
		t.Fatal("Get returned false after Register")
	}
	if got.NfType != NFTypeAMF {
		t.Errorf("NfType = %s, want %s", got.NfType, NFTypeAMF)
	}
	if got.NfStatus != NFStatusRegistered {
		t.Errorf("NfStatus = %s, want %s", got.NfStatus, NFStatusRegistered)
	}
	t.Logf("Register + Get: %s (%s) ✓", got.NfInstanceID, got.NfType)
}

func TestRegistryUpdate(t *testing.T) {
	r := NewRegistry()

	profile := &NFProfile{NfInstanceID: "smf-001", NfType: NFTypeSMF}
	r.Register(profile)

	// Re-register same ID — should return isNew=false
	isNew := r.Register(profile)
	if isNew {
		t.Error("second registration with same ID should return isNew=false")
	}
	if r.Count() != 1 {
		t.Errorf("count = %d, want 1 after update", r.Count())
	}
}

func TestRegistryDeregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&NFProfile{NfInstanceID: "upf-001", NfType: NFTypeUPF})

	if err := r.Deregister("upf-001"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if r.Count() != 0 {
		t.Errorf("count = %d after deregister, want 0", r.Count())
	}

	// Deregistering unknown ID should error
	if err := r.Deregister("unknown"); err == nil {
		t.Error("expected error deregistering unknown ID")
	}
}

func TestRegistryHeartbeat(t *testing.T) {
	r := NewRegistry()
	r.Register(&NFProfile{NfInstanceID: "amf-002", NfType: NFTypeAMF})

	time.Sleep(5 * time.Millisecond) // ensure time moves

	if err := r.Heartbeat("amf-002"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	if err := r.Heartbeat("nonexistent"); err == nil {
		t.Error("expected error heartbeating unknown NF")
	}
}

func TestRegistryDiscover(t *testing.T) {
	r := NewRegistry()

	r.Register(&NFProfile{
		NfInstanceID: "amf-001",
		NfType:       NFTypeAMF,
		PlmnList:     []string{"00101"},
		SNssais:      []Snssai{{Sst: 1}},
	})
	r.Register(&NFProfile{
		NfInstanceID: "smf-001",
		NfType:       NFTypeSMF,
		PlmnList:     []string{"00101"},
		SNssais:      []Snssai{{Sst: 1}},
	})
	r.Register(&NFProfile{
		NfInstanceID: "smf-002",
		NfType:       NFTypeSMF,
		PlmnList:     []string{"99999"}, // different PLMN
	})

	// Discover all SMFs
	results := r.Discover(NFTypeSMF, NFTypeAMF, "", nil)
	if len(results) != 2 {
		t.Errorf("discover all SMFs: got %d, want 2", len(results))
	}

	// Discover SMFs for PLMN 00101 only
	results = r.Discover(NFTypeSMF, NFTypeAMF, "00101", nil)
	if len(results) != 1 {
		t.Errorf("discover SMFs for PLMN 00101: got %d, want 1", len(results))
	}

	// Discover by slice
	results = r.Discover(NFTypeSMF, NFTypeAMF, "", &Snssai{Sst: 1})
	if len(results) != 1 {
		t.Errorf("discover SMFs for SST=1: got %d, want 1", len(results))
	}

	t.Logf("Discovery filtering works correctly ✓")
}

// --- HTTP API Tests ---

func newTestServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	n := New(DefaultConfig())
	srv := httptest.NewServer(buildMux(n))
	client := NewClient(srv.URL)
	t.Cleanup(srv.Close)
	return srv, client
}

// buildMux extracts the HTTP mux setup from Start() so tests can use it
// without binding a real port.
func buildMux(n *NRF) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances/", n.handleNFManagement)
	mux.HandleFunc("/nnrf-disc/v1/nf-instances", n.handleNFDiscovery)
	return mux
}

func TestHTTPRegisterAndDiscover(t *testing.T) {
	_, client := newTestServer(t)

	profile := NFProfile{
		NfInstanceID:  "amf-http-001",
		NfType:        NFTypeAMF,
		PlmnList:      []string{"00101"},
		IPv4Addresses: []string{"127.0.0.1"},
	}

	// Register via HTTP
	stored, err := client.Register(profile)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if stored.NfStatus != NFStatusRegistered {
		t.Errorf("status = %s, want REGISTERED", stored.NfStatus)
	}

	// Discover via HTTP
	result, err := client.Discover(NFTypeAMF, NFTypeSMF, "00101")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.NfInstances) != 1 {
		t.Errorf("discover returned %d instances, want 1", len(result.NfInstances))
	}
	if result.NfInstances[0].NfInstanceID != "amf-http-001" {
		t.Errorf("instance ID = %s, want amf-http-001", result.NfInstances[0].NfInstanceID)
	}

	t.Logf("HTTP Register + Discover: %s ✓", stored.NfInstanceID)
}

func TestHTTPDeregister(t *testing.T) {
	_, client := newTestServer(t)

	profile := NFProfile{NfInstanceID: "smf-http-001", NfType: NFTypeSMF}
	if _, err := client.Register(profile); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := client.Deregister("smf-http-001"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	// Should no longer be discoverable
	result, err := client.Discover(NFTypeSMF, NFTypeAMF, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.NfInstances) != 0 {
		t.Errorf("expected 0 instances after deregister, got %d", len(result.NfInstances))
	}

	t.Log("HTTP Deregister ✓")
}

func TestHTTPHeartbeat(t *testing.T) {
	_, client := newTestServer(t)

	profile := NFProfile{NfInstanceID: "udm-001", NfType: NFTypeUDM}
	if _, err := client.Register(profile); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := client.Heartbeat("udm-001"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	t.Log("HTTP Heartbeat ✓")
}
