// Package udm implements a simplified Unified Data Management function:
// subscriber allowlist and AMF 3GPP access registration (Nudm_UECM).
//
// Ref: TS 29.503 — Nudm services
package udm

import "time"

// ErrorResponse is ProblemDetails-style JSON.
// Ref: TS 29.571 §5.2.6
type ErrorResponse struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// Snssai is slice selection info.
type Snssai struct {
	Sst int    `json:"sst"`
	Sd  string `json:"sd,omitempty"`
}

// Subscriber is provisioned UE subscription data.
type Subscriber struct {
	SUPI          string   `yaml:"supi" json:"supi"`
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	AllowedDnns   []string `yaml:"allowed_dnns" json:"allowedDnns"`
	DefaultSnssai Snssai   `yaml:"default_snssai" json:"defaultSnssai"`
}

// subscribersFile is the YAML root for configs/subscribers.yaml.
type subscribersFile struct {
	Subscribers []Subscriber `yaml:"subscribers"`
}

// Amf3GppAccessRegistration is the PUT body for AMF registration.
// Ref: TS 29.503 — Amf3GppAccessRegistration
type Amf3GppAccessRegistration struct {
	AmfInstanceID string `json:"amfInstanceId"`
}

// SubscriptionData is returned to AMF after successful registration lookup.
type SubscriptionData struct {
	SUPI          string   `json:"supi"`
	AllowedDnns   []string `json:"allowedDnns"`
	DefaultSnssai Snssai   `json:"defaultSnssai"`
}

// UeRegistration tracks AMF's registration of a UE at UDM.
type UeRegistration struct {
	SUPI          string    `json:"supi"`
	AmfInstanceID string    `json:"amfInstanceId"`
	RegisteredAt  time.Time `json:"registeredAt"`
}
