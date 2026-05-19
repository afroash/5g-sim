// manager.go — multi-UE supervisor: spawn, track, and stop UE instances.
package ue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// InstanceState is the lifecycle state of one supervised UE.
type InstanceState string

const (
	StateStarting   InstanceState = "STARTING"
	StateRegistering InstanceState = "REGISTERING"
	StateRegistered InstanceState = "REGISTERED"
	StatePDUActive  InstanceState = "PDU_ACTIVE"
	StateStopped    InstanceState = "STOPPED"
	StateFailed     InstanceState = "FAILED"
)

// InstanceRecord is the public view of one UE instance (API / observatory).
type InstanceRecord struct {
	ID        string        `json:"id"`
	SUPI      string        `json:"supi"`
	IMSI      string        `json:"imsi"`
	State     InstanceState `json:"state"`
	IP        string        `json:"ip,omitempty"`
	Profile   string        `json:"profile,omitempty"`
	TunName   string        `json:"tunName,omitempty"`
	UplinkTEID   uint32     `json:"uplinkTeid,omitempty"`
	DownlinkTEID uint32     `json:"downlinkTeid,omitempty"`
	StartedAt time.Time     `json:"startedAt,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// SpawnOptions selects profile and identity for a new instance.
type SpawnOptions struct {
	Profile string
	SUPI    string
}

// Manager supervises multiple UE instances in one process.
type Manager struct {
	mu         sync.Mutex
	instances  map[string]*managedInstance
	nextSerial int
	base       Config
	profile    string
}

type managedInstance struct {
	record InstanceRecord
	ue     *UE
	cancel context.CancelFunc
}

// NewManager creates a supervisor with base connectivity from cfg and profile name.
func NewManager(base Config, profile string) *Manager {
	if profile == "" {
		profile = ProfileLocal
	}
	return &Manager{
		instances: make(map[string]*managedInstance),
		base:      base,
		profile:   profile,
	}
}

// Spawn starts a new UE instance. SUPI is auto-generated when empty.
// The optional ctx is not used for the instance lifecycle (HTTP handlers must not
// pass r.Context()); instances run until Stop() or process shutdown.
func (m *Manager) Spawn(_ context.Context, opts SpawnOptions) (InstanceRecord, error) {
	profile := strings.ToLower(strings.TrimSpace(opts.Profile))
	if profile == "" {
		profile = m.profile
	}
	base, err := BaseConfigForProfile(profile)
	if err != nil {
		return InstanceRecord{}, err
	}

	m.mu.Lock()
	m.nextSerial++
	serial := m.nextSerial
	m.mu.Unlock()

	supi := strings.TrimSpace(opts.SUPI)
	if supi == "" {
		supi = fmt.Sprintf("imsi-001010000000%03d", serial)
	}
	id := fmt.Sprintf("UE-%03d", serial)
	tunName := fmt.Sprintf("ue%d", serial-1)

	cfg := base
	cfg.SUPI = supi
	cfg.InstanceID = id
	cfg.TunName = tunName

	rec := InstanceRecord{
		ID:        id,
		SUPI:      supi,
		IMSI:      imsiDigits(supi),
		State:     StateStarting,
		Profile:   profile,
		TunName:   tunName,
		UplinkTEID: 1,
		StartedAt: time.Now(),
	}

	instCtx, cancel := context.WithCancel(context.Background())
	u := New(cfg)
	u.onStateChange = func(st InstanceState) {
		m.setState(id, st)
	}
	u.onPDUActive = func(ip string, dlTEID uint32) {
		m.setPDU(id, ip, dlTEID)
	}

	mi := &managedInstance{record: rec, ue: u, cancel: cancel}
	m.mu.Lock()
	m.instances[id] = mi
	m.mu.Unlock()

	go func() {
		err := u.Run(instCtx)
		m.mu.Lock()
		defer m.mu.Unlock()
		cur, ok := m.instances[id]
		if !ok {
			return
		}
		switch {
		case err == nil:
			return
		case errors.Is(err, context.Canceled):
			if cur.record.State != StatePDUActive {
				cur.record.State = StateStopped
			}
			fmt.Printf("[UE supervisor] %s stopped (%v)\n", id, err)
		default:
			cur.record.State = StateFailed
			cur.record.Error = err.Error()
			fmt.Printf("[UE supervisor] %s failed: %v\n", id, err)
		}
	}()

	return m.snapshot(mi), nil
}

// SpawnDefault starts UE-001 with the manager's base profile unless it already exists.
func (m *Manager) SpawnDefault(ctx context.Context) (InstanceRecord, error) {
	m.mu.Lock()
	for _, inst := range m.instances {
		if inst.record.ID == "UE-001" && inst.record.State != StateStopped && inst.record.State != StateFailed {
			rec := inst.record
			m.mu.Unlock()
			return rec, nil
		}
	}
	m.mu.Unlock()

	return m.Spawn(ctx, SpawnOptions{
		Profile: m.profile,
		SUPI:    m.base.SUPI,
	})
}

// Stop terminates an instance by id.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	inst, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("ue: instance %s not found", id)
	}
	cancel := inst.cancel
	m.mu.Unlock()

	cancel()
	inst.ue.Close()
	m.mu.Lock()
	inst.record.State = StateStopped
	m.mu.Unlock()
	return nil
}

// List returns all instance records.
func (m *Manager) List() []InstanceRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]InstanceRecord, 0, len(m.instances))
	for _, inst := range m.instances {
		out = append(out, m.snapshot(inst))
	}
	return out
}

// Get returns one instance or false.
func (m *Manager) Get(id string) (InstanceRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[id]
	if !ok {
		return InstanceRecord{}, false
	}
	return m.snapshot(inst), true
}

func (m *Manager) setState(id string, st InstanceState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[id]; ok {
		inst.record.State = st
	}
}

func (m *Manager) setPDU(id, ip string, dlTEID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[id]; ok {
		inst.record.IP = ip
		inst.record.DownlinkTEID = dlTEID
		inst.record.State = StatePDUActive
	}
}

func (m *Manager) snapshot(inst *managedInstance) InstanceRecord {
	rec := inst.record
	if inst.ue != nil {
		if ip := inst.ue.AllocatedIP(); ip != "" {
			rec.IP = ip
		}
		if teid := inst.ue.DownlinkTEID(); teid != 0 {
			rec.DownlinkTEID = teid
		}
	}
	return rec
}

func imsiDigits(supi string) string {
	if len(supi) >= 5 && strings.EqualFold(supi[:5], "imsi-") {
		return supi[5:]
	}
	return supi
}
