// pfcp_client.go — SMF-side client for the UPF's PFCP-sim API.
//
// After creating a PDU session, the SMF must tell the UPF about it
// so the UPF can register the TEID handler and start forwarding packets.
//
// In a real network this is PFCP over UDP (TS 29.244). Here we use
// a simple HTTP POST — same relationship, simpler protocol.
//
// Ref: TS 29.244 §5.2 — PFCP Session Establishment
// Ref: TS 23.502 §4.3.2.2.1 step 4 — SMF selects UPF
package smf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PFCPClient calls the UPF's PFCP-sim API.
type PFCPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPFCPClient creates a client pointing at the UPF's PFCP-sim endpoint.
// e.g. NewPFCPClient("http://127.0.0.1:8002")
func NewPFCPClient(baseURL string) *PFCPClient {
	return &PFCPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// PFCPSessionRequest mirrors upf.SessionRequest — kept local to avoid
// circular imports between smf and upf packages.
type PFCPSessionRequest struct {
	ULTEID      uint32 `json:"ulTeid"`
	DLTEID      uint32 `json:"dlTeid"`
	GNBAddress  string `json:"gnbAddress"`
	UEIPAddress string `json:"ueIpAddress"`
}

// EstablishSession notifies the UPF about a new UE session.
// The UPF registers the TEID and prepares to forward packets.
//
// Ref: TS 29.244 §5.2.1 — PFCP Session Establishment Request
func (c *PFCPClient) EstablishSession(req PFCPSessionRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := c.baseURL + "/pfcp-sim/v1/sessions"
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("UPF returned %s", resp.Status)
	}

	fmt.Printf("[SMF] PFCP session established with UPF: UL-TEID=0x%08X UE=%s\n",
		req.ULTEID, req.UEIPAddress)
	return nil
}
