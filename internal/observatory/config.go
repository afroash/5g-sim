package observatory

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// NFEndpoint is one network function health probe.
type NFEndpoint struct {
	ID        string `yaml:"id"`
	Label     string `yaml:"label,omitempty"`
	Sub       string `yaml:"sub,omitempty"`
	Spec      string `yaml:"spec,omitempty"`
	HealthURL string `yaml:"health_url"`
}

// Config holds observatory server settings.
type Config struct {
	BindAddress string       `yaml:"bind_address"`
	Port        int          `yaml:"port"`
	AMFObsURL   string       `yaml:"amf_obs_url"`
	UESupervisorURL string   `yaml:"ue_supervisor_url"`
	AutoSpawnDefaultUE bool  `yaml:"auto_spawn_default_ue"`
	DefaultUEProfile   string `yaml:"default_ue_profile"`
	RepoRoot    string       `yaml:"repo_root"`
	NFs         []NFEndpoint `yaml:"nfs"`
	EventBuffer int          `yaml:"event_buffer"`
}

// DefaultConfig returns settings for local NF simulation.
func DefaultConfig() Config {
	return Config{
		BindAddress:        "127.0.0.1",
		Port:               9090,
		AMFObsURL:          "http://127.0.0.1:8090/obs/v1/ues",
		UESupervisorURL:    "http://127.0.0.1:9080",
		AutoSpawnDefaultUE: true,
		DefaultUEProfile:   "local",
		EventBuffer:        500,
		NFs: []NFEndpoint{
			{ID: "NRF", Label: "NRF", Sub: "Network Repository Function", Spec: "TS 29.510", HealthURL: "http://127.0.0.1:8000/health"},
			{ID: "AMF", Label: "AMF", Sub: "Access & Mobility Management", Spec: "TS 29.518", HealthURL: "http://127.0.0.1:8090/health"},
			{ID: "SMF", Label: "SMF", Sub: "Session Management Function", Spec: "TS 29.502", HealthURL: "http://127.0.0.1:8001/health"},
			{ID: "UPF", Label: "UPF", Sub: "User Plane Function", Spec: "TS 29.244", HealthURL: "http://127.0.0.1:8002/health"},
			{ID: "gNB", Label: "gNB", Sub: "Next-Gen NodeB", Spec: "TS 38.413", HealthURL: "http://127.0.0.1:8003/health"},
			{ID: "UDM", Label: "UDM", Sub: "Unified Data Management", Spec: "TS 29.503", HealthURL: "http://127.0.0.1:8004/health"},
		},
	}
}

// LoadConfig reads YAML merged over defaults.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("observatory: read config %s: %w", path, err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("observatory: parse config %s: %w", path, err)
	}
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = 500
	}
	return cfg, nil
}

// ListenAddr returns host:port for http.ListenAndServe.
func (c Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.BindAddress, c.Port)
}
