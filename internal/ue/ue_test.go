// ue_test.go — Tests for the standalone UE package.
package ue

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig verifies that DefaultConfig returns sensible values.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SUPI == "" {
		t.Error("DefaultConfig SUPI is empty")
	}
	if cfg.GNBAddress == "" {
		t.Error("DefaultConfig GNBAddress is empty")
	}
	if cfg.GNBSCTPPort == 0 {
		t.Error("DefaultConfig GNBSCTPPort is 0")
	}
	if cfg.DNN == "" {
		t.Error("DefaultConfig DNN is empty")
	}
	t.Logf("DefaultConfig: SUPI=%s gNB=%s:%d DNN=%s ✓",
		cfg.SUPI, cfg.GNBAddress, cfg.GNBSCTPPort, cfg.DNN)
}

// TestLoadConfig verifies that a YAML config file overrides defaults.
func TestLoadConfig(t *testing.T) {
	yaml := `
supi: "imsi-001010000000099"
gnb_address: "10.1.1.1"
gnb_sctp_port: 38413
gnb_gtp_address: "10.1.1.1:2153"
dnn: "internet"
slice:
  sst: 1
  sd: "000001"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "ue.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.SUPI != "imsi-001010000000099" {
		t.Errorf("SUPI = %q, want %q", cfg.SUPI, "imsi-001010000000099")
	}
	if cfg.GNBAddress != "10.1.1.1" {
		t.Errorf("GNBAddress = %q, want %q", cfg.GNBAddress, "10.1.1.1")
	}
	if cfg.GNBSCTPPort != 38413 {
		t.Errorf("GNBSCTPPort = %d, want 38413", cfg.GNBSCTPPort)
	}
	if cfg.DNN != "internet" {
		t.Errorf("DNN = %q, want %q", cfg.DNN, "internet")
	}
	if cfg.Slice.SST != 1 {
		t.Errorf("Slice.SST = %d, want 1", cfg.Slice.SST)
	}

	t.Logf("LoadConfig: SUPI=%s gNB=%s:%d DNN=%s ✓",
		cfg.SUPI, cfg.GNBAddress, cfg.GNBSCTPPort, cfg.DNN)
}

// TestLoadConfig_Missing verifies that a missing file returns an error.
func TestLoadConfig_Missing(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/ue.yaml")
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
	t.Logf("missing config correctly rejected: %v ✓", err)
}
