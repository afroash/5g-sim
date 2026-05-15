// Package obspub publishes observability events to the 5G Observatory sidecar.
//
// Set OBSERVATORY_URL (e.g. http://127.0.0.1:9090) before starting NFs, or call
// Configure explicitly. Events are sent asynchronously; failures are dropped.
package obspub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/afroash/5g-sim/pkg/obslog"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// Event is the JSON payload POSTed to /api/v1/events.
type Event struct {
	ID        string            `json:"id"`
	TS        time.Time         `json:"ts"`
	Kind      string            `json:"kind"`
	From      string            `json:"from,omitempty"`
	To        string            `json:"to,omitempty"`
	Type      string            `json:"type,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Spec      string            `json:"spec,omitempty"`
	Component string            `json:"component,omitempty"`
	Level     string            `json:"level,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

var (
	mu      sync.RWMutex
	baseURL string
	client  = &http.Client{Timeout: 2 * time.Second}
	seq     uint64
)

func init() {
	if u := os.Getenv("OBSERVATORY_URL"); u != "" {
		Configure(u)
	}
	obslog.SetPublishHook(func(e obslog.Entry) {
		if Enabled() {
			Emit(fromLogEntry(e))
		}
	})
}

// Configure sets the observatory base URL (e.g. http://127.0.0.1:9090).
// Empty URL disables publishing.
func Configure(url string) {
	mu.Lock()
	defer mu.Unlock()
	baseURL = trimURL(url)
}

// Enabled reports whether publishing is active.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return baseURL != ""
}

func trimURL(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}

func nextID() string {
	mu.Lock()
	seq++
	n := seq
	mu.Unlock()
	return time.Now().Format("20060102150405") + "-" + itoa(n)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Emit sends an event to the observatory without blocking the caller.
func Emit(ev Event) {
	mu.RLock()
	url := baseURL
	mu.RUnlock()
	if url == "" {
		return
	}
	if ev.ID == "" {
		ev.ID = nextID()
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now()
	}
	go post(url+"/api/v1/events", ev)
}

func post(url string, ev Event) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// fromLogEntry maps an obslog entry to a GUI event.
func fromLogEntry(e obslog.Entry) Event {
	return Event{
		ID:        nextID(),
		TS:        e.Timestamp,
		Kind:      "log",
		Type:      e.Message,
		Detail:    e.Message,
		Spec:      e.SpecRef,
		Component: e.Component,
		Level:     stringsTrim(e.Level),
		Fields:    e.Fields,
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

// FromProcedure maps a sequence-diagram procedure step to a GUI event.
func FromProcedure(from, to seqdiag.Node, label, specRef string, fields map[string]string) Event {
	return Event{
		ID:        nextID(),
		TS:        time.Now(),
		Kind:      "procedure",
		From:      string(from),
		To:        string(to),
		Type:      label,
		Detail:    label,
		Spec:      specRef,
		Component: string(from),
		Level:     "INFO",
		Fields:    fields,
	}
}
