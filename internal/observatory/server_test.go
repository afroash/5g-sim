package observatory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/gorilla/websocket"
)

func TestIngestAndWebSocket(t *testing.T) {
	cfg := DefaultConfig()
	hub := NewHub(50)
	poller := NewPoller(cfg)
	ues := NewUEManager(cfg)
	srv := NewServer(cfg, hub, poller, ues, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]interface{}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap["type"] != "snapshot" {
		t.Fatalf("first frame type %v", snap["type"])
	}

	ev := obspub.Event{
		ID:   "test-1",
		Kind: "procedure",
		From: "gNB",
		To:   "AMF",
		Type: "NGSetupRequest",
		Spec: "TS 38.413",
		TS:   time.Now(),
	}
	body, _ := json.Marshal(ev)
	resp, err := http.Post(ts.URL+"/api/v1/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest status %d", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, raw, err = conn.ReadMessage()
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatal("no event frame received")
			}
			continue
		}
		var msg map[string]interface{}
		_ = json.Unmarshal(raw, &msg)
		if msg["type"] == "event" {
			return
		}
		if msg["type"] == "topology" {
			continue
		}
	}
}
