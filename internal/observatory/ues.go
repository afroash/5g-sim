package observatory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/afroash/5g-sim/internal/ue"
	"gopkg.in/yaml.v3"
)

// UERecord is one UE known to the observatory (AMF or spawned locally).
type UERecord struct {
	ID           string    `json:"id"`
	IMSI         string    `json:"imsi"`
	State        string    `json:"state"`
	IP           string    `json:"ip,omitempty"`
	GNB          string    `json:"gnb,omitempty"`
	PDUSession   string    `json:"pduSession,omitempty"`
	Source       string    `json:"source"`
	Profile      string    `json:"profile,omitempty"` // spawn preset: local | clab
	PID          int       `json:"pid,omitempty"`
	SpawnedAt    time.Time `json:"spawnedAt,omitempty"`
}

// SpawnUEOptions selects how the observatory generates the temporary ue.yaml when spawning ./cmd/ue.
type SpawnUEOptions struct {
	Profile string `json:"profile"` // ue.ProfileLocal or ue.ProfileCLab (default local)
	SUPI    string `json:"supi"`    // optional; omit for auto SERIAL
}

// AMFUE is the JSON shape from GET /obs/v1/ues on the AMF.
type AMFUE struct {
	SUPI         string `json:"supi"`
	State        string `json:"state"`
	AllocatedIP  string `json:"allocatedIp"`
	SMContextRef string `json:"smContextRef,omitempty"`
	RegisteredAt string `json:"registeredAt,omitempty"`
}

// AMFUESnapshot is the AMF observability response.
type AMFUESnapshot struct {
	UEs []AMFUE `json:"ues"`
}

// UEManager tracks spawned UE processes and merges AMF state.
type UEManager struct {
	cfg        Config
	client     *http.Client
	mu         sync.Mutex
	spawned    map[string]*spawnedUE
	nextSerial int
}

type spawnedUE struct {
	record UERecord
	cmd    *exec.Cmd
}

// NewUEManager creates a UE spawn tracker.
func NewUEManager(cfg Config) *UEManager {
	return &UEManager{
		cfg:     cfg,
		client:  &http.Client{Timeout: 2 * time.Second},
		spawned: make(map[string]*spawnedUE),
	}
}

// List returns AMF UEs plus locally spawned processes.
func (m *UEManager) List(ctx context.Context) ([]UERecord, error) {
	var out []UERecord

	if m.cfg.AMFObsURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.AMFObsURL, nil)
		if err == nil {
			resp, err := m.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var snap AMFUESnapshot
					if json.NewDecoder(resp.Body).Decode(&snap) == nil {
						for i, u := range snap.UEs {
							imsi := u.SUPI
							if len(imsi) > 5 && imsi[:5] == "imsi-" {
								imsi = imsi[5:]
							}
							out = append(out, UERecord{
								ID:         fmt.Sprintf("UE-%03d", i+1),
								IMSI:       imsi,
								State:      u.State,
								IP:         u.AllocatedIP,
								GNB:        "gNB",
								PDUSession: u.SMContextRef,
								Source:     "amf",
							})
						}
					}
				}
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.spawned {
		if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
			s.record.State = "STOPPED"
		}
		rec := s.record
		rec.ID = id
		out = append(out, rec)
	}
	return out, nil
}

// ParseSpawnUEBody parses the optional POST /api/v1/ues JSON ({ "profile", "supi" }).
func ParseSpawnUEBody(raw []byte) (SpawnUEOptions, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return SpawnUEOptions{Profile: ue.ProfileLocal}, nil
	}
	var req SpawnUEOptions
	if err := json.Unmarshal(raw, &req); err != nil {
		return SpawnUEOptions{}, err
	}
	prof := strings.ToLower(strings.TrimSpace(req.Profile))
	if prof == "" {
		prof = ue.ProfileLocal
	}
	if _, err := ue.BaseConfigForProfile(prof); err != nil {
		return SpawnUEOptions{}, err
	}
	req.Profile = prof
	req.SUPI = strings.TrimSpace(req.SUPI)
	return req, nil
}

// Spawn starts a UE subprocess with a generated or caller-supplied SUPI and connection profile.
func (m *UEManager) Spawn(ctx context.Context, opts SpawnUEOptions) (UERecord, error) {
	profile := strings.ToLower(strings.TrimSpace(opts.Profile))
	if profile == "" {
		profile = ue.ProfileLocal
	}
	if _, err := ue.BaseConfigForProfile(profile); err != nil {
		return UERecord{}, err
	}

	m.mu.Lock()
	m.nextSerial++
	serial := m.nextSerial
	m.mu.Unlock()

	var supi string
	if opts.SUPI != "" {
		supi = opts.SUPI
	} else {
		supi = fmt.Sprintf("imsi-001010000000%03d", serial)
	}
	id := fmt.Sprintf("UE-%03d", serial)

	cfgPath, err := m.writeUEConfig(supi, profile)
	if err != nil {
		return UERecord{}, err
	}

	root := m.cfg.RepoRoot
	if root == "" {
		root, _ = os.Getwd()
	}

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/ue", "-config", cfgPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "OBSERVATORY_URL=") // avoid feedback loop from UE logs if any

	if err := cmd.Start(); err != nil {
		return UERecord{}, fmt.Errorf("spawn ue: %w", err)
	}

	rec := UERecord{
		ID:        id,
		IMSI:      imsiDigitsWithoutPrefix(supi),
		State:     "STARTING",
		GNB:       "gNB",
		Source:    "spawned",
		Profile:   profile,
		PID:       cmd.Process.Pid,
		SpawnedAt: time.Now(),
	}

	m.mu.Lock()
	m.spawned[id] = &spawnedUE{record: rec, cmd: cmd}
	m.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if s, ok := m.spawned[id]; ok {
			s.record.State = "STOPPED"
		}
		m.mu.Unlock()
	}()

	return rec, nil
}

// Stop terminates a spawned UE by id.
func (m *UEManager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.spawned[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("ue %s not found", id)
	}
	cmd := s.cmd
	m.mu.Unlock()

	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

func imsiDigitsWithoutPrefix(supi string) string {
	if len(supi) >= 5 && strings.EqualFold(supi[:5], "imsi-") {
		return supi[5:]
	}
	return supi
}

func (m *UEManager) writeUEConfig(supi string, profile string) (string, error) {
	dir := filepath.Join(os.TempDir(), "5g-sim-observatory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	safeBase := filepath.Base(strings.ReplaceAll(supi, ":", "_"))
	path := filepath.Join(dir, safeBase+".yaml")

	cfg, err := ue.BaseConfigForProfile(profile)
	if err != nil {
		return "", err
	}
	cfg.SUPI = supi

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := yaml.NewEncoder(f).Encode(&cfg); err != nil {
		return "", err
	}
	return path, nil
}

// Reap removes exited spawned UEs from the map.
func (m *UEManager) Reap() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.spawned {
		if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
			delete(m.spawned, id)
		}
	}
}

// FetchAMF is a helper for tests.
func FetchAMF(ctx context.Context, url string) ([]AMFUE, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("amf obs: %s", string(b))
	}
	var snap AMFUESnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, err
	}
	return snap.UEs, nil
}
