// registry.go — In-memory NF instance registry.
//
// Stores NFProfiles keyed by nfInstanceId (UUID).
// Thread-safe — multiple NFs register concurrently.
//
// Ref: TS 29.510 §5.3.2 — NF Registration
package nrf

import (
	"fmt"
	"sync"
	"time"
)

// Registry is the in-memory store of all registered NF instances.
// In a production NRF this would be backed by a database, but for
// our simulator an in-memory map is sufficient and much simpler.
type Registry struct {
	mu       sync.RWMutex
	profiles map[string]*NFProfile // key: nfInstanceId
}

// NewRegistry creates an empty NF registry.
func NewRegistry() *Registry {
	return &Registry{
		profiles: make(map[string]*NFProfile),
	}
}

// Register stores or replaces an NF profile.
// Called when an NF POSTs to /nnrf-nfm/v1/nf-instances/{nfInstanceId}.
//
// If the instance already exists this is a full update (PUT semantics).
// Returns true if this is a new registration, false if an update.
//
// Ref: TS 29.510 §5.3.2.2 — NF Registration (PUT)
func (r *Registry) Register(profile *NFProfile) (isNew bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.profiles[profile.NfInstanceID]

	profile.RegisteredAt = time.Now()
	profile.LastHeartbeat = time.Now()
	profile.NfStatus = NFStatusRegistered

	r.profiles[profile.NfInstanceID] = profile

	if !exists {
		fmt.Printf("[NRF] Registered new NF: type=%s id=%s\n",
			profile.NfType, profile.NfInstanceID)
	} else {
		fmt.Printf("[NRF] Updated NF profile: type=%s id=%s\n",
			profile.NfType, profile.NfInstanceID)
	}

	return !exists
}

// Heartbeat updates the LastHeartbeat timestamp for a registered NF.
// Called when an NF PATCHes its profile to signal it's still alive.
//
// Ref: TS 29.510 §5.3.2.4 — NF Heartbeat
func (r *Registry) Heartbeat(nfInstanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	profile, ok := r.profiles[nfInstanceID]
	if !ok {
		return fmt.Errorf("NF instance %s not found", nfInstanceID)
	}

	profile.LastHeartbeat = time.Now()
	fmt.Printf("[NRF] Heartbeat from NF: type=%s id=%s\n",
		profile.NfType, nfInstanceID)
	return nil
}

// Deregister removes an NF profile from the registry.
// Called when an NF DELETEs its registration on graceful shutdown.
//
// Ref: TS 29.510 §5.3.2.3 — NF Deregistration
func (r *Registry) Deregister(nfInstanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	profile, ok := r.profiles[nfInstanceID]
	if !ok {
		return fmt.Errorf("NF instance %s not found", nfInstanceID)
	}

	fmt.Printf("[NRF] Deregistered NF: type=%s id=%s\n",
		profile.NfType, nfInstanceID)
	delete(r.profiles, nfInstanceID)
	return nil
}

// Get returns a single NF profile by instance ID.
func (r *Registry) Get(nfInstanceID string) (*NFProfile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.profiles[nfInstanceID]
	return p, ok
}

// Discover returns all registered NF profiles matching the given criteria.
// Empty criteria fields are ignored (wildcard).
//
// Ref: TS 29.510 §5.3.3 — NF Discovery
func (r *Registry) Discover(targetNFType NFType, requesterNFType NFType, plmn string, snssai *Snssai) []NFProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []NFProfile

	for _, profile := range r.profiles {
		// Must match target NF type
		if targetNFType != "" && profile.NfType != targetNFType {
			continue
		}

		// Must be in REGISTERED state
		if profile.NfStatus != NFStatusRegistered {
			continue
		}

		// Must serve the requested PLMN (if specified)
		if plmn != "" && !containsPLMN(profile.PlmnList, plmn) {
			continue
		}

		// Must support the requested slice (if specified)
		if snssai != nil && !containsSnssai(profile.SNssais, snssai) {
			continue
		}

		// Must allow the requester NF type (if AllowedNfTypes is set)
		if requesterNFType != "" && len(profile.AllowedNfTypes) > 0 {
			if !containsNFType(profile.AllowedNfTypes, requesterNFType) {
				continue
			}
		}

		results = append(results, *profile)
	}

	fmt.Printf("[NRF] Discovery: target=%s requester=%s → %d results\n",
		targetNFType, requesterNFType, len(results))

	return results
}

// Count returns the total number of registered NF instances.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.profiles)
}

// --- helpers ---

func containsPLMN(list []string, plmn string) bool {
	for _, p := range list {
		if p == plmn {
			return true
		}
	}
	return false
}

func containsSnssai(list []Snssai, target *Snssai) bool {
	for _, s := range list {
		if s.Sst == target.Sst && (target.Sd == "" || s.Sd == target.Sd) {
			return true
		}
	}
	return false
}

func containsNFType(list []NFType, target NFType) bool {
	for _, t := range list {
		if t == target {
			return true
		}
	}
	return false
}
