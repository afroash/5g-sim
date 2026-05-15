package obspub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/afroash/5g-sim/pkg/seqdiag"
)

func TestEmitPOST(t *testing.T) {
	var got Event
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	Configure(srv.URL)
	defer Configure("")

	Emit(Event{Kind: "test", Type: "ping", From: "AMF", To: "gNB"})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		ok := got.Kind == "test"
		mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event not received")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFromProcedure(t *testing.T) {
	ev := FromProcedure(seqdiag.NodeGNB, seqdiag.NodeAMF, "NGSetupRequest", "TS 38.413", nil)
	if ev.From != "gNB" || ev.To != "AMF" || ev.Kind != "procedure" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}
