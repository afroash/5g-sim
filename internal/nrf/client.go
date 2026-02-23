// client.go — HTTP client for NF registration and discovery.
//
// Used by AMF, SMF, and other NFs to:
//   - Register themselves with the NRF on startup
//   - Send periodic heartbeats
//   - Discover other NF instances
//
// Ref: TS 29.510 §5.3.2 — NF Registration
// Ref: TS 29.510 §5.3.3 — NF Discovery
package nrf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is an HTTP client for communicating with the NRF.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new NRF client pointing at the given base URL.
// e.g. NewClient("http://127.0.0.1:8000")
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register sends a PUT request to register this NF instance with the NRF.
// Returns the stored profile as confirmed by the NRF.
//
// Ref: TS 29.510 §5.3.2.2
func (c *Client) Register(profile NFProfile) (*NFProfile, error) {
	body, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, profile.NfInstanceID)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PUT %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("NRF register failed: %s — %s", resp.Status, body)
	}

	var stored NFProfile
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	fmt.Printf("[NRF Client] Registered as %s (id=%s)\n", profile.NfType, profile.NfInstanceID)
	return &stored, nil
}

// Heartbeat sends a PATCH to the NRF to signal this NF is still alive.
// Should be called periodically (typically every 60s).
//
// Ref: TS 29.510 §5.3.2.4
func (c *Client) Heartbeat(nfInstanceID string) error {
	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, nfInstanceID)
	req, err := http.NewRequest(http.MethodPatch, url, nil)
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat failed: %s", resp.Status)
	}

	return nil
}

// Deregister sends a DELETE to remove this NF from the registry.
// Called on graceful shutdown.
//
// Ref: TS 29.510 §5.3.2.3
func (c *Client) Deregister(nfInstanceID string) error {
	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, nfInstanceID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create deregister request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("deregister failed: %s", resp.Status)
	}

	fmt.Printf("[NRF Client] Deregistered NF instance %s\n", nfInstanceID)
	return nil
}

// Discover queries the NRF for NF instances matching the given criteria.
// Any empty field is treated as a wildcard.
//
// Ref: TS 29.510 §5.3.3
func (c *Client) Discover(targetNFType, requesterNFType NFType, plmn string) (*DiscoveryResponse, error) {
	params := url.Values{}
	if targetNFType != "" {
		params.Set("target-nf-type", string(targetNFType))
	}
	if requesterNFType != "" {
		params.Set("requester-nf-type", string(requesterNFType))
	}
	if plmn != "" {
		params.Set("plmn-id", plmn)
	}

	endpoint := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances?%s", c.baseURL, params.Encode())

	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery failed: %s — %s", resp.Status, body)
	}

	var result DiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode discovery response: %w", err)
	}

	fmt.Printf("[NRF Client] Discovery: target=%s → %d instances found\n",
		targetNFType, len(result.NfInstances))
	return &result, nil
}
