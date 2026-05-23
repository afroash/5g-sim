package upf

import (
	"os"
	"strings"
)

// Data-plane modes for UPF N6 (kernel TUN vs in-process simulation).
const (
	DataPlaneModeAuto       = "auto"
	DataPlaneModeFabric     = "fabric"
	DataPlaneModeStandalone = "standalone"
)

func normalizeDataPlaneMode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (c Config) effectiveDataPlaneMode() string {
	if v := os.Getenv("UPF_DATA_PLANE_MODE"); v != "" {
		return normalizeDataPlaneMode(v)
	}
	m := normalizeDataPlaneMode(c.DataPlaneMode)
	if m == "" {
		return DataPlaneModeAuto
	}
	return m
}
