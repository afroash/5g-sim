package amf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleObsUEsLocalOnly(t *testing.T) {
	a := New(DefaultConfig())
	a.ues.Add(&UEContext{
		SUPI:        "imsi-001010000000001",
		State:       UEStateRegistered,
		AllocatedIP: "10.0.0.2",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/obs/v1/ues", a.handleObsUEs)

	// Remote client forbidden
	req := httptest.NewRequest(http.MethodGet, "/obs/v1/ues", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote: status %d want 403", w.Code)
	}

	// Localhost allowed
	req = httptest.NewRequest(http.MethodGet, "/obs/v1/ues", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("local: status %d want 200", w.Code)
	}
	var body struct {
		UEs []obsUE `json:"ues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.UEs) != 1 || body.UEs[0].SUPI != "imsi-001010000000001" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
