package observatory

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// NFStatus is the health of one NF.
type NFStatus struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Sub       string `json:"sub,omitempty"`
	Spec      string `json:"spec,omitempty"`
	HealthURL string `json:"healthUrl"`
	Status    string `json:"status"`
}

// TopologySnapshot is returned by GET /api/v1/topology.
type TopologySnapshot struct {
	NFs       []NFStatus `json:"nfs"`
	Online    int        `json:"online"`
	Total     int        `json:"total"`
	CheckedAt time.Time  `json:"checkedAt"`
}

// Poller periodically probes NF health endpoints.
type Poller struct {
	cfg    Config
	client *http.Client
	mu     sync.RWMutex
	last   TopologySnapshot
}

// NewPoller creates a topology health poller.
func NewPoller(cfg Config) *Poller {
	return &Poller{
		cfg:    cfg,
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

// Poll checks all configured NF health URLs once.
func (p *Poller) Poll(ctx context.Context) TopologySnapshot {
	out := TopologySnapshot{
		Total:     len(p.cfg.NFs),
		CheckedAt: time.Now(),
	}
	for _, nf := range p.cfg.NFs {
		st := NFStatus{
			ID:        nf.ID,
			Label:     nf.Label,
			Sub:       nf.Sub,
			Spec:      nf.Spec,
			HealthURL: nf.HealthURL,
			Status:    "down",
		}
		if nf.Label == "" {
			st.Label = nf.ID
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nf.HealthURL, nil)
		if err == nil {
			resp, err := p.client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					st.Status = "up"
					out.Online++
				}
			}
		}
		out.NFs = append(out.NFs, st)
	}
	p.mu.Lock()
	p.last = out
	p.mu.Unlock()
	return out
}

// Last returns the most recent snapshot.
func (p *Poller) Last() TopologySnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.last
}

// Run polls on interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	p.Poll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Poll(ctx)
		}
	}
}
