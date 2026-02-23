// smf_test.go — Tests for SMF IP pool, session store, and HTTP API.
package smf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- IP Pool Tests ---

func TestIPPoolAllocate(t *testing.T) {
	pool, err := NewIPPool("10.1.0.0/24")
	if err != nil {
		t.Fatalf("NewIPPool: %v", err)
	}

	ip1, err := pool.Allocate("imsi-001")
	if err != nil {
		t.Fatalf("first Allocate: %v", err)
	}
	if ip1 != "10.1.0.1" {
		t.Errorf("first IP = %s, want 10.1.0.1", ip1)
	}

	ip2, err := pool.Allocate("imsi-002")
	if err != nil {
		t.Fatalf("second Allocate: %v", err)
	}
	if ip2 != "10.1.0.2" {
		t.Errorf("second IP = %s, want 10.1.0.2", ip2)
	}

	if pool.Count() != 2 {
		t.Errorf("count = %d, want 2", pool.Count())
	}

	t.Logf("IP pool: allocated %s and %s ✓", ip1, ip2)
}

func TestIPPoolRelease(t *testing.T) {
	pool, _ := NewIPPool("10.2.0.0/24")

	ip, _ := pool.Allocate("imsi-001")
	pool.Release(ip)

	if pool.Count() != 0 {
		t.Errorf("count = %d after release, want 0", pool.Count())
	}
}

func TestIPPoolExhaustion(t *testing.T) {
	// /30 gives only 2 usable addresses
	pool, err := NewIPPool("10.3.0.0/30")
	if err != nil {
		t.Fatalf("NewIPPool: %v", err)
	}

	pool.Allocate("imsi-001")
	pool.Allocate("imsi-002")

	_, err = pool.Allocate("imsi-003")
	if err == nil {
		t.Error("expected error when pool is exhausted")
	}
	t.Logf("Pool exhaustion correctly detected ✓")
}

// --- Session Store Tests ---

func TestSessionStoreAddGet(t *testing.T) {
	store := NewSessionStore()

	ctx := &SmContext{
		SUPI:           "imsi-001010000000001",
		PDUSessionID:   1,
		DNN:            "internet",
		PDUSessionType: PDUSessionTypeIPv4,
		AllocatedIP:    "10.0.0.1",
		Status:         PDUSessionStatusActive,
	}

	id := store.Add(ctx)
	if id == "" {
		t.Fatal("Add returned empty ID")
	}

	got, ok := store.Get(id)
	if !ok {
		t.Fatalf("Get(%s) returned false", id)
	}
	if got.AllocatedIP != "10.0.0.1" {
		t.Errorf("AllocatedIP = %s, want 10.0.0.1", got.AllocatedIP)
	}

	t.Logf("Session store: add/get %s ✓", id)
}

func TestSessionStoreDelete(t *testing.T) {
	store := NewSessionStore()
	ctx := &SmContext{SUPI: "imsi-001", PDUSessionID: 1}
	id := store.Add(ctx)
	store.Delete(id)

	if store.Count() != 0 {
		t.Errorf("count = %d after delete, want 0", store.Count())
	}
}

// --- HTTP API Tests ---

func newTestSMF(t *testing.T) (*SMF, *httptest.Server, *Client) {
	t.Helper()
	s, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New SMF: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", s.handleSMContexts)
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", s.handleSMContextByID)

	srv := httptest.NewServer(mux)
	client := NewClient(srv.URL)
	t.Cleanup(srv.Close)
	return s, srv, client
}

func TestHTTPCreateSMContext(t *testing.T) {
	_, _, client := newTestSMF(t)

	req := SmContextCreateRequest{
		Supi:           "imsi-001010000000001",
		PDUSessionID:   1,
		Dnn:            "internet",
		PDUSessionType: PDUSessionTypeIPv4,
		ServingNfID:    "amf-001",
		ServingNetwork: "00101",
		SNssai:         SNssai{Sst: 1},
	}

	resp, err := client.CreateSMContext(req)
	if err != nil {
		t.Fatalf("CreateSMContext: %v", err)
	}

	if resp.SmContextRef == "" {
		t.Error("SmContextRef should not be empty")
	}
	if resp.PDUAddress == nil {
		t.Fatal("PDUAddress should not be nil")
	}
	if resp.PDUAddress.Ipv4Addr == "" {
		t.Error("Ipv4Addr should not be empty")
	}

	t.Logf("HTTP CreateSMContext: ip=%s ref=%s ✓",
		resp.PDUAddress.Ipv4Addr, resp.SmContextRef)
}

func TestHTTPReleaseSMContext(t *testing.T) {
	smf, srv, client := newTestSMF(t)

	req := SmContextCreateRequest{
		Supi: "imsi-002", PDUSessionID: 1,
		Dnn: "internet", PDUSessionType: PDUSessionTypeIPv4,
	}
	resp, err := client.CreateSMContext(req)
	if err != nil {
		t.Fatalf("CreateSMContext: %v", err)
	}

	// Release using the full ref URL — but rewrite to test server URL
	ctxID := resp.SmContextRef[len(resp.SmContextRef)-9:] // last 9 chars = "ctx-00001"
	releaseURL := srv.URL + "/nsmf-pdusession/v1/sm-contexts/" + ctxID

	if err := client.ReleaseSMContext(releaseURL); err != nil {
		t.Fatalf("ReleaseSMContext: %v", err)
	}

	if smf.sessions.Count() != 0 {
		t.Errorf("session count = %d after release, want 0", smf.sessions.Count())
	}
	if smf.pool.Count() != 0 {
		t.Errorf("pool count = %d after release, want 0", smf.pool.Count())
	}

	t.Log("HTTP ReleaseSMContext ✓")
}

func TestHTTPCreateSMContextMultiple(t *testing.T) {
	_, _, client := newTestSMF(t)

	ips := make(map[string]bool)
	for i := 1; i <= 5; i++ {
		req := SmContextCreateRequest{
			Supi:           fmt.Sprintf("imsi-00101000000000%d", i),
			PDUSessionID:   i,
			Dnn:            "internet",
			PDUSessionType: PDUSessionTypeIPv4,
		}
		resp, err := client.CreateSMContext(req)
		if err != nil {
			t.Fatalf("CreateSMContext[%d]: %v", i, err)
		}
		ip := resp.PDUAddress.Ipv4Addr
		if ips[ip] {
			t.Errorf("IP %s allocated twice", ip)
		}
		ips[ip] = true
	}

	t.Logf("5 unique IPs allocated: %v ✓", ips)
}

// Keep compiler happy for the JSON body test
var _ = bytes.NewReader
var _ = json.NewEncoder
var _ = http.MethodPost
