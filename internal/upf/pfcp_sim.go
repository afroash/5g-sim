// pfcp_sim.go — Simplified PFCP interface for the UPF.
//
// In a real network the SMF sends PFCP Session Establishment Requests
// to the UPF over N4 (UDP port 8805). We simulate this with a small
// HTTP API that lets the SMF register sessions with the UPF directly.
//
// This avoids implementing the full PFCP binary protocol while still
// correctly modelling the architectural relationship:
//
//	SMF → (N4/PFCP) → UPF: "here is a new session, TEID=X, UE IP=Y"
//
// Ref: TS 29.244 — PFCP (Packet Forwarding Control Protocol)
// Ref: TS 23.501 §6.2.3 — UPF responsibilities
package upf

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// SessionRequest is the body the SMF sends to register a UE session.
type SessionRequest struct {
	// ULTEID is the TEID the gNB will use for uplink packets.
	// The UPF registers a handler for this TEID.
	ULTEID uint32 `json:"ulTeid"`

	// DLTEID is the TEID the UPF should use when sending downlink to the gNB.
	DLTEID uint32 `json:"dlTeid"`

	// GNBAddress is the gNB GTP-U endpoint ("ip:port") for downlink.
	GNBAddress string `json:"gnbAddress"`

	// UEIPAddress is the UE's allocated IP (for logging/filtering).
	UEIPAddress string `json:"ueIpAddress"`
}

// StartPFCPSim starts the UPF's PFCP simulation HTTP server.
// Listens on the given port (default 8002).
func (u *UPF) StartPFCPSim(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/pfcp-sim/v1/sessions", u.handleSessionCreate)
	mux.HandleFunc("/pfcp-sim/v1/sessions/", u.handleSessionDelete)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[UPF] PFCP-sim HTTP server listening on %s\n", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Printf("[UPF] PFCP-sim server error: %v\n", err)
		}
	}()
	return nil
}

// handleSessionCreate registers a new UE session.
// POST /pfcp-sim/v1/sessions
func (u *UPF) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse gNB address for downlink
	var gnbAddr *net.UDPAddr
	if req.GNBAddress != "" {
		host, portStr, err := net.SplitHostPort(req.GNBAddress)
		if err == nil {
			var port int
			fmt.Sscanf(portStr, "%d", &port)
			gnbAddr = &net.UDPAddr{IP: net.ParseIP(host), Port: port}
		}
	}

	u.mu.Lock()
	existing, exists := u.sessions[req.ULTEID]
	if exists && req.GNBAddress != "" {
		// Update existing session with gNB downlink info from the gNB-side
		// PFCP notify (sent by the gNB once its UPF-facing socket is bound).
		existing.GNBAddr = gnbAddr
		existing.GNTEID = req.DLTEID
		if req.UEIPAddress != "" && existing.UEIPAddress != req.UEIPAddress {
			delete(u.sessionsByUEIP, existing.UEIPAddress)
			existing.UEIPAddress = req.UEIPAddress
			u.sessionsByUEIP[req.UEIPAddress] = existing
		}
		u.mu.Unlock()
		fmt.Printf("[UPF] PFCP-sim: session updated UL-TEID=0x%08X DL-TEID=0x%08X gNB=%s\n",
			req.ULTEID, req.DLTEID, req.GNBAddress)
	} else {
		u.mu.Unlock()
		sess := &UPFSession{
			TEID:        req.ULTEID,
			GNTEID:      req.DLTEID,
			GNBAddr:     gnbAddr,
			UEIPAddress: req.UEIPAddress,
		}
		u.RegisterSession(sess)
		fmt.Printf("[UPF] PFCP-sim: session registered UL-TEID=0x%08X UE=%s gNB=%s\n",
			req.ULTEID, req.UEIPAddress, req.GNBAddress)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSessionDelete removes a session by TEID.
// DELETE /pfcp-sim/v1/sessions/{teid}
func (u *UPF) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var teid uint32
	fmt.Sscanf(r.URL.Path[len("/pfcp-sim/v1/sessions/"):], "%d", &teid)

	u.mu.Lock()
	if sess, ok := u.sessions[teid]; ok && sess.UEIPAddress != "" {
		delete(u.sessionsByUEIP, sess.UEIPAddress)
	}
	delete(u.sessions, teid)
	u.mu.Unlock()
	u.tunnel.DeregisterTEID(teid)

	fmt.Printf("[UPF] PFCP-sim: session released TEID=0x%08X\n", teid)
	w.WriteHeader(http.StatusNoContent)
}
