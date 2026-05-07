// config.go — UE simulator configuration.
package ue

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the UE's startup configuration.
type Config struct {
	// SUPI is the UE's permanent identity, e.g. "imsi-001010000000001".
	// Ref: TS 23.003 §2.2B
	SUPI string `yaml:"supi"`

	// GNBAddress is the IP address of the gNB to connect to.
	GNBAddress string `yaml:"gnb_address"`

	// GNBSCTPPort is the gNB's SCTP port for UE connections.
	GNBSCTPPort int `yaml:"gnb_sctp_port"`

	// GNBGTPAddress is the gNB's GTP-U endpoint for user plane traffic ("ip:port").
	// Ref: TS 29.281 §4.4
	GNBGTPAddress string `yaml:"gnb_gtp_address"`

	// DNN is the Data Network Name for the PDU session, e.g. "internet".
	// Ref: TS 23.003 §9A
	DNN string `yaml:"dnn"`

	// Slice is the network slice the UE requests.
	// Ref: TS 23.003 §28
	Slice SliceConfig `yaml:"slice"`
}

// SliceConfig identifies a 5G network slice.
type SliceConfig struct {
	SST uint8  `yaml:"sst"` // Slice/Service Type: 1=eMBB
	SD  string `yaml:"sd"`  // Slice Differentiator: "000001" or "ffffff"
}

// DefaultConfig returns a configuration for local development.
func DefaultConfig() Config {
	return Config{
		SUPI:          "imsi-001010000000001",
		GNBAddress:    "127.0.0.1",
		GNBSCTPPort:   38413,
		GNBGTPAddress: "127.0.0.1:2153",
		DNN:           "internet",
		Slice:         SliceConfig{SST: 1, SD: "000001"},
	}
}

// LoadConfig reads a YAML file and returns a Config merged over DefaultConfig.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("ue: read config %s: %w", path, err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("ue: parse config %s: %w", path, err)
	}
	return cfg, nil
}
