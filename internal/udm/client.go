package udm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an HTTP client for Nudm_UECM (AMF and optional UE preflight).
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a client, e.g. NewClient("http://127.0.0.1:8004").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RegisterAMF3GPPAccess registers the UE at UDM during AMF registration.
// Ref: TS 29.503 — Nudm_UECM_Registration
func (c *Client) RegisterAMF3GPPAccess(supi, amfInstanceID string) (*SubscriptionData, error) {
	body, _ := json.Marshal(Amf3GppAccessRegistration{AmfInstanceID: amfInstanceID})
	url := fmt.Sprintf("%s/nudm-uecm/v1/%s/registrations/amf-3gpp-access", c.baseURL, supi)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PUT %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("UDM: subscriber not provisioned: %s", b)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("UDM returned %s: %s", resp.Status, b)
	}

	var data SubscriptionData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode UDM response: %w", err)
	}
	return &data, nil
}

// GetSubscription checks that a SUPI is provisioned (UE preflight).
func (c *Client) GetSubscription(supi string) (*SubscriptionData, error) {
	url := fmt.Sprintf("%s/nudm-uecm/v1/%s", c.baseURL, supi)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("subscriber not provisioned: %s", supi)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("UDM GET %s: %s", resp.Status, b)
	}
	var data SubscriptionData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}
