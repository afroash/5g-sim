// config.go — UE simulator configuration.
package ue

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Known connection presets for spawning UEs locally or against ContainerLab NFs.
// Ref: docs/observatory-ue-modes.md Phase A — profile is data-only; YAML file still overlays.
const (
	ProfileLocal = "local"
	ProfileCLab  = "clab"
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

	// UDMAddress is optional; when set, UE verifies SUPI is provisioned before attach.
	UDMAddress string `yaml:"udm_address"`

	// InstanceID is the supervisor-assigned id (e.g. UE-001).
	InstanceID string `yaml:"instance_id"`

	// TunName is the kernel TUN device name (e.g. ue0, ue1).
	TunName string `yaml:"tun_name"`

	// UplinkTEID is the TEID used toward the gNB (default 1).
	UplinkTEID uint32 `yaml:"uplink_teid"`

	// DataPlaneMode: "auto" (default TUN, else userspace), "fabric" (TUN required), "standalone" (skip TUN).
	DataPlaneMode string `yaml:"data_plane_mode"`

	// ConnectivityTargetAddr is the ping/HTTP target (default 10.100.0.1 / internet-sim).
	ConnectivityTargetAddr string `yaml:"connectivity_target_addr"`
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
		UDMAddress:    "http://127.0.0.1:8004",
	}
}

// DefaultCLabConfig returns presets aligned with deploy/configs/nfs/ue.yaml (CLAB-facing gNB).
func DefaultCLabConfig() Config {
	return Config{
		SUPI:          "imsi-001010000000001",
		GNBAddress:    "10.1.1.1",
		GNBSCTPPort:   38413,
		GNBGTPAddress: "10.1.1.1:2153",
		DNN:           "internet",
		Slice:         SliceConfig{SST: 1, SD: "000001"},
		UDMAddress:    "http://127.0.0.1:8004",
	}
}

// BaseConfigForProfile returns preset connectivity for name: "local" (or ""), or "clab".
func BaseConfigForProfile(name string) (Config, error) {
	switch normalizeProfile(name) {
	case "", ProfileLocal:
		return DefaultConfig(), nil
	case ProfileCLab:
		return DefaultCLabConfig(), nil
	default:
		return Config{}, fmt.Errorf("ue: unknown profile %q (use %q or %q)", strings.TrimSpace(name), ProfileLocal, ProfileCLab)
	}
}

func normalizeProfile(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// LoadConfig reads a YAML file and returns a Config merged over DefaultConfig.
func LoadConfig(path string) (Config, error) {
	return LoadConfigOver(DefaultConfig(), path)
}

// LoadConfigOver merges a YAML file on top of base (omit keys in YAML to keep base values).
func LoadConfigOver(base Config, path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("ue: read config %s: %w", path, err)
	}
	cfg := base
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("ue: parse config %s: %w", path, err)
	}
	return cfg, nil
}

// ConnectivityTarget returns the address used for post-attach connectivity checks.
func (c Config) ConnectivityTarget() string {
	if strings.TrimSpace(c.ConnectivityTargetAddr) != "" {
		return strings.TrimSpace(c.ConnectivityTargetAddr)
	}
	return "10.100.0.1"
}
