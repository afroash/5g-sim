package ue

import (
	"os"
	"strings"
)

const (
	DataPlaneModeAuto       = "auto"
	DataPlaneModeFabric     = "fabric"
	DataPlaneModeStandalone = "standalone"
)

func normalizeUEDataPlaneMode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// EffectiveDataPlaneMode returns YAML/env-selected mode for this UE.
func (c Config) EffectiveDataPlaneMode() string {
	if v := os.Getenv("UE_DATA_PLANE_MODE"); v != "" {
		return normalizeUEDataPlaneMode(v)
	}
	m := normalizeUEDataPlaneMode(c.DataPlaneMode)
	if m == "" {
		return DataPlaneModeAuto
	}
	return m
}
