// client.go — HTTP client for the AMF to call the SMF over N11.
//
// The AMF calls the SMF when a UE requests a PDU session.
// This client wraps the Nsmf_PDUSession HTTP API.
//
// Ref: TS 29.502 — Nsmf_PDUSession
// Ref: TS 23.502 §4.3.2 — PDU Session Establishment
package smf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an HTTP client for calling the SMF's Nsmf_PDUSession API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new SMF client pointing at the given base URL.
// e.g. NewClient("http://127.0.0.1:8001")
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateSMContext asks the SMF to create a PDU session for a UE.
// Returns the created context including the allocated IP address.
//
// Ref: TS 29.502 §5.2.2.2 — Nsmf_PDUSession_CreateSMContext
// Ref: TS 23.502 §4.3.2.2.1 step 3 — AMF→SMF Nsmf_PDUSession_CreateSMContext
func (c *Client) CreateSMContext(req SmContextCreateRequest) (*SmContextCreateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/nsmf-pdusession/v1/sm-contexts", c.baseURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SMF returned %s: %s", resp.Status, b)
	}

	var result SmContextCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	fmt.Printf("[SMF Client] SM context created: ref=%s ip=%s\n",
		result.SmContextRef, result.PDUAddress.Ipv4Addr)
	return &result, nil
}

// ReleaseSMContext asks the SMF to release a PDU session.
// Called when the UE deregisters or explicitly releases the session.
//
// Ref: TS 29.502 §5.2.2.4 — Nsmf_PDUSession_ReleaseSMContext
func (c *Client) ReleaseSMContext(smContextRef string) error {
	req, err := http.NewRequest(http.MethodDelete, smContextRef, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", smContextRef, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SMF returned %s: %s", resp.Status, b)
	}

	fmt.Printf("[SMF Client] SM context released: %s\n", smContextRef)
	return nil
}
