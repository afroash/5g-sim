package udm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	nrfclient "github.com/afroash/5g-sim/internal/nrf"
)

// UDM is the runtime UDM instance.
type UDM struct {
	config   Config
	registry *Registry
}

// New creates a UDM with a loaded subscriber registry.
func New(cfg Config, registry *Registry) *UDM {
	return &UDM{config: cfg, registry: registry}
}

// Registry returns the subscriber registry (for tests).
func (u *UDM) Registry() *Registry {
	return u.registry
}

// Start registers with NRF and serves HTTP.
func (u *UDM) Start() error {
	if err := u.registerWithNRF(); err != nil {
		fmt.Printf("[UDM] NRF registration failed (continuing): %v\n", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/nudm-uecm/v1/", u.handleUECM)

	addr := fmt.Sprintf("%s:%d", u.config.BindAddress, u.config.Port)
	fmt.Printf("[UDM] HTTP server listening on http://%s\n", addr)
	fmt.Printf("[UDM] Subscribers loaded: %d\n", len(u.registry.subscribers))
	fmt.Println("[UDM] Routes:")
	fmt.Println("[UDM]   GET    /nudm-uecm/v1/{supi}")
	fmt.Println("[UDM]   PUT    /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access")
	fmt.Println("[UDM]   DELETE /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access")

	return http.ListenAndServe(addr, mux)
}

func (u *UDM) handleUECM(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/nudm-uecm/v1/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "missing supi in path")
		return
	}

	const regSuffix = "/registrations/amf-3gpp-access"
	if strings.HasSuffix(path, regSuffix) {
		supi := strings.TrimSuffix(path, regSuffix)
		switch r.Method {
		case http.MethodPut:
			u.putAmfRegistration(w, r, supi)
		case http.MethodDelete:
			u.deleteAmfRegistration(w, supi)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	u.getSubscription(w, path)
}

func (u *UDM) getSubscription(w http.ResponseWriter, supi string) {
	sub, ok := u.registry.GetSubscriber(supi)
	if !ok {
		writeError(w, http.StatusNotFound, "subscriber not provisioned")
		return
	}
	writeJSON(w, http.StatusOK, SubscriptionData{
		SUPI:          sub.SUPI,
		AllowedDnns:   sub.AllowedDnns,
		DefaultSnssai: sub.DefaultSnssai,
	})
}

func (u *UDM) putAmfRegistration(w http.ResponseWriter, r *http.Request, supi string) {
	var body Amf3GppAccessRegistration
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	amfID := body.AmfInstanceID
	if amfID == "" {
		amfID = "amf-sim-001"
	}

	data, err := u.registry.RegisterAMF3GPPAccess(supi, amfID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	fmt.Printf("[UDM] AMF 3GPP registration: %s via %s\n", supi, amfID)
	writeJSON(w, http.StatusCreated, data)
}

func (u *UDM) deleteAmfRegistration(w http.ResponseWriter, supi string) {
	if _, ok := u.registry.GetSubscriber(supi); !ok {
		writeError(w, http.StatusNotFound, "subscriber not provisioned")
		return
	}
	u.registry.DeregisterAMF3GPPAccess(supi)
	w.WriteHeader(http.StatusNoContent)
}

func (u *UDM) registerWithNRF() error {
	client := nrfclient.NewClient(u.config.NRFAddress)
	base := fmt.Sprintf("http://%s:%d", u.config.BindAddress, u.config.Port)
	profile := nrfclient.NFProfile{
		NfInstanceID:  u.config.InstanceID,
		NfType:        nrfclient.NFTypeUDM,
		NfStatus:      nrfclient.NFStatusRegistered,
		PlmnList:      []string{"00101"},
		IPv4Addresses: []string{u.config.BindAddress},
		NfServices: []nrfclient.NFService{
			{
				ServiceInstanceID: "nudm-uecm-001",
				ServiceName:       "nudm-uecm",
				Versions:          []string{"v1"},
				Scheme:            "http",
				NFServiceStatus:   nrfclient.NFStatusRegistered,
				APIPrefix:         base,
			},
		},
	}
	_, err := client.Register(profile)
	if err != nil {
		return err
	}
	fmt.Printf("[UDM] Registered with NRF at %s\n", u.config.NRFAddress)
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}
