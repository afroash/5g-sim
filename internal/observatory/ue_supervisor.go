package observatory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/afroash/5g-sim/internal/ue"
)

// supervisorClient talks to the UE supervisor HTTP API (cmd/ue).
type supervisorClient struct {
	baseURL string
	client  *http.Client
}

func newSupervisorClient(baseURL string) *supervisorClient {
	return &supervisorClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *supervisorClient) available() bool {
	return c != nil && c.baseURL != ""
}

func (c *supervisorClient) list(ctx context.Context) ([]ue.InstanceRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/instances", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ue supervisor: %s", string(b))
	}
	var out struct {
		Instances []ue.InstanceRecord `json:"instances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Instances, nil
}

func (c *supervisorClient) spawn(ctx context.Context, opts SpawnUEOptions) (ue.InstanceRecord, error) {
	body, _ := json.Marshal(map[string]string{
		"profile": opts.Profile,
		"supi":    opts.SUPI,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/instances", bytes.NewReader(body))
	if err != nil {
		return ue.InstanceRecord{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return ue.InstanceRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return ue.InstanceRecord{}, fmt.Errorf("ue supervisor: %s", string(b))
	}
	var rec ue.InstanceRecord
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return ue.InstanceRecord{}, err
	}
	return rec, nil
}

func (c *supervisorClient) stop(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/instances/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ue supervisor: %s", string(b))
	}
	return nil
}

func instanceToUERecord(inst ue.InstanceRecord) UERecord {
	state := strings.ToUpper(string(inst.State))
	return UERecord{
		ID:        inst.ID,
		IMSI:      inst.IMSI,
		State:     state,
		IP:        inst.IP,
		GNB:       "gNB",
		PDUSession: pduLabel(inst),
		Source:    "supervisor",
		Profile:   inst.Profile,
		SpawnedAt: inst.StartedAt,
	}
}

func pduLabel(inst ue.InstanceRecord) string {
	if inst.State == ue.StatePDUActive {
		return "PSI-1"
	}
	return ""
}
