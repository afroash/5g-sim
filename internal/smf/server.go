// server.go — SMF HTTP server exposing the Nsmf_PDUSession service.
//
// Implements the N11 interface used by the AMF to create, modify,
// and release PDU sessions.
//
// Routes:
//
//	POST   /nsmf-pdusession/v1/sm-contexts          → Create session
//	GET    /nsmf-pdusession/v1/sm-contexts/{id}     → Get session
//	DELETE /nsmf-pdusession/v1/sm-contexts/{id}     → Release session
//
// Ref: TS 29.502 §6.1 — Nsmf_PDUSession service
package smf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	nrfclient "github.com/afroash/5g-sim/internal/nrf"
)

// Config holds the SMF's startup configuration.
type Config struct {
	// InstanceID is the SMF's UUID for NRF registration.
	InstanceID string

	// Port is the HTTP port for the Nsmf_PDUSession API.
	Port int

	// PLMN is the PLMN this SMF serves.
	PLMN string

	// IPPoolCIDR is the address range for UE IP allocation.
	IPPoolCIDR string

	// NRFAddress is the NRF's base URL for registration.
	NRFAddress string
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		InstanceID: "smf-sim-001",
		Port:       8001,
		PLMN:       "00101",
		IPPoolCIDR: "10.0.0.0/24",
		NRFAddress: "http://127.0.0.1:8000",
	}
}

// SMF is the runtime SMF instance.
type SMF struct {
	config   Config
	pool     *IPPool
	sessions *SessionStore
}

// New creates a new SMF ready to start.
func New(cfg Config) (*SMF, error) {
	pool, err := NewIPPool(cfg.IPPoolCIDR)
	if err != nil {
		return nil, fmt.Errorf("IP pool: %w", err)
	}

	return &SMF{
		config:   cfg,
		pool:     pool,
		sessions: NewSessionStore(),
	}, nil
}

// Start registers with the NRF and begins serving HTTP requests.
func (s *SMF) Start() error {
	fmt.Printf("[SMF] Starting — ID: %s  PLMN: %s  Pool: %s\n",
		s.config.InstanceID, s.config.PLMN, s.config.IPPoolCIDR)

	// Register with NRF so the AMF can discover us.
	// Ref: TS 29.510 §5.3.2
	if err := s.registerWithNRF(); err != nil {
		// Non-fatal — log and continue. NRF may not be running yet.
		fmt.Printf("[SMF] NRF registration failed (continuing): %v\n", err)
	}

	mux := http.NewServeMux()

	// Nsmf_PDUSession service routes
	// Ref: TS 29.502 §6.1.2
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", s.handleSMContexts)
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", s.handleSMContextByID)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	addr := fmt.Sprintf(":%d", s.config.Port)
	fmt.Printf("[SMF] HTTP server listening on %s\n", addr)
	fmt.Println("[SMF] Routes:")
	fmt.Println("[SMF]   POST   /nsmf-pdusession/v1/sm-contexts       → Create session")
	fmt.Println("[SMF]   GET    /nsmf-pdusession/v1/sm-contexts/{id}  → Get session")
	fmt.Println("[SMF]   DELETE /nsmf-pdusession/v1/sm-contexts/{id}  → Release session")

	return http.ListenAndServe(addr, mux)
}

// handleSMContexts handles POST /nsmf-pdusession/v1/sm-contexts
// Routes to session creation.
func (s *SMF) handleSMContexts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createSMContext(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSMContextByID handles GET/DELETE on /nsmf-pdusession/v1/sm-contexts/{id}
func (s *SMF) handleSMContextByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/nsmf-pdusession/v1/sm-contexts/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing context ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getSMContext(w, r, id)
	case http.MethodDelete:
		s.releaseSMContext(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createSMContext handles POST /nsmf-pdusession/v1/sm-contexts
//
// Called by the AMF when a UE requests a PDU session.
// Allocates an IP address and creates a session context.
//
// Ref: TS 29.502 §5.2.2.2 — Nsmf_PDUSession_CreateSMContext
// Ref: TS 23.502 §4.3.2.2.1 — SMF PDU Session Establishment
func (s *SMF) createSMContext(w http.ResponseWriter, r *http.Request) {
	var req SmContextCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	fmt.Printf("[SMF] CreateSMContext: supi=%s pduSessionId=%d dnn=%s type=%s\n",
		req.Supi, req.PDUSessionID, req.Dnn, req.PDUSessionType)

	// Default to IPv4 if not specified
	if req.PDUSessionType == "" {
		req.PDUSessionType = PDUSessionTypeIPv4
	}
	if req.Dnn == "" {
		req.Dnn = "internet"
	}

	// Allocate an IP address from the pool
	// Ref: TS 23.502 §4.3.2.2.1 step 4 — SMF selects UPF and allocates IP
	ip, err := s.pool.Allocate(req.Supi)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable,
			fmt.Sprintf("IP allocation failed: %v", err))
		return
	}

	// Create and store the session context
	ctx := &SmContext{
		SUPI:           req.Supi,
		PDUSessionID:   req.PDUSessionID,
		DNN:            req.Dnn,
		SNssai:         req.SNssai,
		PDUSessionType: req.PDUSessionType,
		AllocatedIP:    ip,
		Status:         PDUSessionStatusActive,
		CreatedAt:      time.Now(),
	}
	ctxID := s.sessions.Add(ctx)

	// Build the response with the context reference and allocated IP
	smfBase := fmt.Sprintf("http://127.0.0.1:%d", s.config.Port)
	resp := SmContextCreateResponse{
		SmContextRef: fmt.Sprintf("%s/nsmf-pdusession/v1/sm-contexts/%s", smfBase, ctxID),
		PDUAddress: &PDUAddress{
			PduSessionType: PDUSessionTypeIPv4,
			Ipv4Addr:       ip,
		},
	}

	fmt.Printf("[SMF] Session created: id=%s ip=%s ✓\n", ctxID, ip)

	// 201 Created — new resource
	// Ref: TS 29.502 §6.1.6.3.2
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", resp.SmContextRef)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// getSMContext handles GET /nsmf-pdusession/v1/sm-contexts/{id}
// Returns the current state of a session context.
// Ref: TS 29.502 §5.2.2.5
func (s *SMF) getSMContext(w http.ResponseWriter, r *http.Request, id string) {
	ctx, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("SM context %s not found", id))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ctx)
}

// releaseSMContext handles DELETE /nsmf-pdusession/v1/sm-contexts/{id}
//
// Called by AMF when a UE deregisters or releases the session.
// Returns the IP address to the pool.
//
// Ref: TS 29.502 §5.2.2.4 — Nsmf_PDUSession_ReleaseSMContext
func (s *SMF) releaseSMContext(w http.ResponseWriter, r *http.Request, id string) {
	ctx, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("SM context %s not found", id))
		return
	}

	s.pool.Release(ctx.AllocatedIP)
	s.sessions.Delete(id)

	fmt.Printf("[SMF] Session released: id=%s ip=%s\n", id, ctx.AllocatedIP)
	w.WriteHeader(http.StatusNoContent)
}

// registerWithNRF registers this SMF instance with the NRF.
// Ref: TS 29.510 §5.3.2
func (s *SMF) registerWithNRF() error {
	client := nrfclient.NewClient(s.config.NRFAddress)

	profile := nrfclient.NFProfile{
		NfInstanceID:  s.config.InstanceID,
		NfType:        nrfclient.NFTypeSMF,
		NfStatus:      nrfclient.NFStatusRegistered,
		PlmnList:      []string{s.config.PLMN},
		IPv4Addresses: []string{"127.0.0.1"},
		NfServices: []nrfclient.NFService{
			{
				ServiceInstanceID: "nsmf-pdusession-1",
				ServiceName:       "nsmf-pdusession",
				Versions:          []string{"v1"},
				Scheme:            "http",
				NFServiceStatus:   nrfclient.NFStatusRegistered,
				APIPrefix:         fmt.Sprintf("http://127.0.0.1:%d", s.config.Port),
			},
		},
	}

	if _, err := client.Register(profile); err != nil {
		return err
	}

	fmt.Printf("[SMF] Registered with NRF at %s\n", s.config.NRFAddress)
	return nil
}

// writeError writes a JSON problem detail error response.
func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}
