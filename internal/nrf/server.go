// server.go — NRF HTTP REST API server.
//
// Implements two services from TS 29.510:
//
//   NF Management (nnrf-nfm):
//     PUT    /nnrf-nfm/v1/nf-instances/{nfInstanceId}   → Register/Update
//     GET    /nnrf-nfm/v1/nf-instances/{nfInstanceId}   → Get profile
//     DELETE /nnrf-nfm/v1/nf-instances/{nfInstanceId}   → Deregister
//     PATCH  /nnrf-nfm/v1/nf-instances/{nfInstanceId}   → Heartbeat
//
//   NF Discovery (nnrf-disc):
//     GET    /nnrf-disc/v1/nf-instances                 → Discover NFs
//
// Ref: TS 29.510 §6 — NRF API definition
package nrf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config holds the NRF's startup configuration.
type Config struct {
	// BindAddress is the IP to listen on; "" binds all interfaces.
	BindAddress string `yaml:"bind_address"`

	// Port is the HTTP port to listen on.
	Port int `yaml:"port"`

	// ValidityPeriod is how long discovery results are valid (seconds).
	// Ref: TS 29.510 §6.1.6.2.36
	ValidityPeriod int `yaml:"validity_period"`
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		BindAddress:    "",
		Port:           8000,
		ValidityPeriod: 3600,
	}
}

// NRF is the runtime NRF instance.
type NRF struct {
	config   Config
	registry *Registry
}

// New creates a new NRF with an empty registry.
func New(cfg Config) *NRF {
	return &NRF{
		config:   cfg,
		registry: NewRegistry(),
	}
}

// Registry returns the NRF's internal registry (for testing).
func (n *NRF) Registry() *Registry {
	return n.registry
}

// Start boots the NRF HTTP server and blocks.
func (n *NRF) Start() error {
	mux := http.NewServeMux()

	// NF Management routes
	// Ref: TS 29.510 §6.1.2
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances/", n.handleNFManagement)

	// NF Discovery route
	// Ref: TS 29.510 §6.2.2
	mux.HandleFunc("/nnrf-disc/v1/nf-instances", n.handleNFDiscovery)

	// Health check — not in spec, useful for readiness probes
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	addr := fmt.Sprintf("%s:%d", n.config.BindAddress, n.config.Port)
	fmt.Printf("[NRF] HTTP server listening on %s\n", addr)
	fmt.Println("[NRF] Routes:")
	fmt.Println("[NRF]   PUT    /nnrf-nfm/v1/nf-instances/{id}  → Register")
	fmt.Println("[NRF]   GET    /nnrf-nfm/v1/nf-instances/{id}  → Get profile")
	fmt.Println("[NRF]   DELETE /nnrf-nfm/v1/nf-instances/{id}  → Deregister")
	fmt.Println("[NRF]   PATCH  /nnrf-nfm/v1/nf-instances/{id}  → Heartbeat")
	fmt.Println("[NRF]   GET    /nnrf-disc/v1/nf-instances       → Discover")

	return http.ListenAndServe(addr, mux)
}

// handleNFManagement dispatches PUT/GET/DELETE/PATCH on /nnrf-nfm/v1/nf-instances/{id}
// Ref: TS 29.510 §6.1
func (n *NRF) handleNFManagement(w http.ResponseWriter, r *http.Request) {
	// Extract nfInstanceId from path: /nnrf-nfm/v1/nf-instances/{nfInstanceId}
	prefix := "/nnrf-nfm/v1/nf-instances/"
	nfInstanceID := strings.TrimPrefix(r.URL.Path, prefix)
	if nfInstanceID == "" {
		writeError(w, http.StatusBadRequest, "missing nfInstanceId in path")
		return
	}

	switch r.Method {
	case http.MethodPut:
		n.registerNF(w, r, nfInstanceID)
	case http.MethodGet:
		n.getNF(w, r, nfInstanceID)
	case http.MethodDelete:
		n.deregisterNF(w, r, nfInstanceID)
	case http.MethodPatch:
		n.heartbeatNF(w, r, nfInstanceID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// registerNF handles PUT /nnrf-nfm/v1/nf-instances/{nfInstanceId}
//
// An NF registers by PUTting its full NFProfile. The NRF stores it
// and returns 201 Created (new) or 200 OK (update).
//
// Ref: TS 29.510 §5.3.2.2 — NF Registration
func (n *NRF) registerNF(w http.ResponseWriter, r *http.Request, nfInstanceID string) {
	var profile NFProfile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	// Enforce the nfInstanceId from the URL path — ignore what's in the body
	// to prevent spoofing. Ref: TS 29.510 §6.1.3.2
	profile.NfInstanceID = nfInstanceID

	isNew := n.registry.Register(&profile)

	w.Header().Set("Content-Type", "application/json")
	if isNew {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(profile)
}

// getNF handles GET /nnrf-nfm/v1/nf-instances/{nfInstanceId}
//
// Returns the stored profile for a given NF instance.
// Ref: TS 29.510 §5.3.2.5 — NF Profile Retrieval
func (n *NRF) getNF(w http.ResponseWriter, r *http.Request, nfInstanceID string) {
	profile, ok := n.registry.Get(nfInstanceID)
	if !ok {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("NF instance %s not found", nfInstanceID))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// deregisterNF handles DELETE /nnrf-nfm/v1/nf-instances/{nfInstanceId}
//
// Called by an NF on graceful shutdown to remove itself from the registry.
// Ref: TS 29.510 §5.3.2.3 — NF Deregistration
func (n *NRF) deregisterNF(w http.ResponseWriter, r *http.Request, nfInstanceID string) {
	if err := n.registry.Deregister(nfInstanceID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// heartbeatNF handles PATCH /nnrf-nfm/v1/nf-instances/{nfInstanceId}
//
// NFs send a heartbeat periodically to signal they're still alive.
// The NRF updates LastHeartbeat. In a full implementation, NFs that
// miss heartbeats would be marked as SUSPENDED.
//
// Ref: TS 29.510 §5.3.2.4 — NF Heartbeat
func (n *NRF) heartbeatNF(w http.ResponseWriter, r *http.Request, nfInstanceID string) {
	if err := n.registry.Heartbeat(nfInstanceID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"heartbeatAckTimer": fmt.Sprintf("%d", int(time.Minute.Seconds())),
	})
}

// handleNFDiscovery handles GET /nnrf-disc/v1/nf-instances
//
// Query parameters (all optional):
//   target-nf-type    — filter by NF type (e.g. "SMF")
//   requester-nf-type — who is asking (for AllowedNfTypes check)
//   plmn-id           — filter by PLMN (e.g. "00101")
//   snssais           — filter by slice SST (e.g. "1")
//
// Ref: TS 29.510 §5.3.3 — NF Discovery
func (n *NRF) handleNFDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	targetNFType := NFType(q.Get("target-nf-type"))
	requesterNFType := NFType(q.Get("requester-nf-type"))
	plmn := q.Get("plmn-id")

	// Parse snssai SST if provided
	var snssai *Snssai
	if sstStr := q.Get("snssais"); sstStr != "" {
		var sst int
		fmt.Sscanf(sstStr, "%d", &sst)
		snssai = &Snssai{Sst: sst}
	}

	instances := n.registry.Discover(targetNFType, requesterNFType, plmn, snssai)

	resp := DiscoveryResponse{
		ValidityPeriod: n.config.ValidityPeriod,
		NfInstances:    instances,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeError writes a JSON problem detail response.
// Ref: TS 29.571 §5.2.6 — ProblemDetails
func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}
