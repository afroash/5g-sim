package udm

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds UDM startup settings.
type Config struct {
	BindAddress      string `yaml:"bind_address"`
	Port             int    `yaml:"port"`
	NRFAddress       string `yaml:"nrf_address"`
	InstanceID       string `yaml:"instance_id"`
	SubscribersPath  string `yaml:"subscribers_path"`
}

// DefaultConfig returns local development defaults.
func DefaultConfig() Config {
	return Config{
		BindAddress:     "127.0.0.1",
		Port:            8004,
		NRFAddress:      "http://127.0.0.1:8000",
		InstanceID:      "udm-sim-001",
		SubscribersPath: "configs/subscribers.yaml",
	}
}

// LoadConfig reads YAML merged over defaults.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("udm: read config %s: %w", path, err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("udm: parse config %s: %w", path, err)
	}
	return cfg, nil
}
