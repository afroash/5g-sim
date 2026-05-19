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

// UEManager tracks UE instances via the UE supervisor and merges AMF state.
type UEManager struct {
	cfg        Config
	client     *http.Client
	supervisor *supervisorClient
	mu         sync.Mutex
	spawned    map[string]*spawnedUE // legacy exec fallback only
	nextSerial int
}

type spawnedUE struct {
	record UERecord
	cmd    *exec.Cmd
}

// NewUEManager creates a UE spawn tracker.
func NewUEManager(cfg Config) *UEManager {
	var sup *supervisorClient
	if cfg.UESupervisorURL != "" {
		sup = newSupervisorClient(cfg.UESupervisorURL)
	}
	return &UEManager{
		cfg:        cfg,
		client:     &http.Client{Timeout: 2 * time.Second},
		supervisor: sup,
		spawned:    make(map[string]*spawnedUE),
	}
}

// List returns supervisor/AMF UEs (merged by SUPI).
func (m *UEManager) List(ctx context.Context) ([]UERecord, error) {
	var out []UERecord

	if m.supervisor != nil && m.supervisor.available() {
		insts, err := m.supervisor.list(ctx)
		if err == nil {
			for _, inst := range insts {
				out = append(out, instanceToUERecord(inst))
			}
		}
	}

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
						rec := amfUEToRecord(i, u)
						if idx := findUERecordBySUPI(out, u.SUPI); idx >= 0 {
							mergeAMFIntoRecord(&out[idx], rec)
						} else {
							out = append(out, rec)
						}
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

// Spawn starts a UE via the supervisor API, or falls back to a local subprocess.
func (m *UEManager) Spawn(ctx context.Context, opts SpawnUEOptions) (UERecord, error) {
	profile := strings.ToLower(strings.TrimSpace(opts.Profile))
	if profile == "" {
		profile = ue.ProfileLocal
	}
	if _, err := ue.BaseConfigForProfile(profile); err != nil {
		return UERecord{}, err
	}

	if m.supervisor != nil && m.supervisor.available() {
		inst, err := m.supervisor.spawn(ctx, opts)
		if err != nil {
			return UERecord{}, err
		}
		return instanceToUERecord(inst), nil
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

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/ue", "-instance", "-config", cfgPath)
	cmd.Dir = root
	cmd.Env = os.Environ()

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

// Stop terminates a UE via the supervisor or local subprocess map.
func (m *UEManager) Stop(id string) error {
	if m.supervisor != nil && m.supervisor.available() {
		return m.supervisor.stop(context.Background(), id)
	}
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

// EnsureDefaultUE spawns the default UE via supervisor when configured and none exist.
func (m *UEManager) EnsureDefaultUE(ctx context.Context) error {
	if !m.cfg.AutoSpawnDefaultUE {
		return nil
	}
	if m.supervisor == nil || !m.supervisor.available() {
		return nil
	}
	list, err := m.supervisor.list(ctx)
	if err != nil {
		return err
	}
	if len(list) > 0 {
		return nil
	}
	prof := m.cfg.DefaultUEProfile
	if prof == "" {
		prof = ue.ProfileLocal
	}
	_, err = m.supervisor.spawn(ctx, SpawnUEOptions{Profile: prof, SUPI: "imsi-001010000000001"})
	return err
}

func amfUEToRecord(i int, u AMFUE) UERecord {
	imsi := u.SUPI
	if len(imsi) > 5 && imsi[:5] == "imsi-" {
		imsi = imsi[5:]
	}
	return UERecord{
		ID:         fmt.Sprintf("UE-%03d", i+1),
		IMSI:       imsi,
		State:      u.State,
		IP:         u.AllocatedIP,
		GNB:        "gNB",
		PDUSession: u.SMContextRef,
		Source:     "amf",
	}
}

func findUERecordBySUPI(list []UERecord, supi string) int {
	digits := imsiDigitsWithoutPrefix(supi)
	for i, r := range list {
		if r.IMSI == digits || r.IMSI == supi {
			return i
		}
	}
	return -1
}

func mergeAMFIntoRecord(dst *UERecord, amf UERecord) {
	if dst.IP == "" && amf.IP != "" {
		dst.IP = amf.IP
	}
	if amf.State == "REGISTERED" || amf.State == "PDU_ACTIVE" {
		dst.State = amf.State
	}
	if dst.PDUSession == "" {
		dst.PDUSession = amf.PDUSession
	}
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
