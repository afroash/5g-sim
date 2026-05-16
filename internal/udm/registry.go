package udm

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Registry holds provisioned subscribers and runtime AMF registrations.
type Registry struct {
	mu            sync.RWMutex
	subscribers   map[string]*Subscriber
	registrations map[string]*UeRegistration
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		subscribers:   make(map[string]*Subscriber),
		registrations: make(map[string]*UeRegistration),
	}
}

// LoadSubscribersFromFile reads configs/subscribers.yaml (or path).
func LoadSubscribersFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read subscribers %s: %w", path, err)
	}
	var file subscribersFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse subscribers %s: %w", path, err)
	}
	r := NewRegistry()
	for i := range file.Subscribers {
		sub := file.Subscribers[i]
		if sub.SUPI == "" {
			continue
		}
		key := normalizeSUPI(sub.SUPI)
		cp := sub
		r.subscribers[key] = &cp
	}
	if len(r.subscribers) == 0 {
		return nil, fmt.Errorf("no subscribers in %s", path)
	}
	return r, nil
}

func normalizeSUPI(supi string) string {
	return strings.ToLower(strings.TrimSpace(supi))
}

// GetSubscriber returns provisioned data or false.
func (r *Registry) GetSubscriber(supi string) (*Subscriber, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sub, ok := r.subscribers[normalizeSUPI(supi)]
	if !ok || !sub.Enabled {
		return nil, false
	}
	return sub, true
}

// RegisterAMF3GPPAccess records AMF UE registration if subscriber exists.
func (r *Registry) RegisterAMF3GPPAccess(supi, amfInstanceID string) (*SubscriptionData, error) {
	sub, ok := r.GetSubscriber(supi)
	if !ok {
		return nil, fmt.Errorf("subscriber not found: %s", supi)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := normalizeSUPI(supi)
	r.registrations[key] = &UeRegistration{
		SUPI:          key,
		AmfInstanceID: amfInstanceID,
		RegisteredAt:  time.Now(),
	}
	return &SubscriptionData{
		SUPI:          sub.SUPI,
		AllowedDnns:   append([]string(nil), sub.AllowedDnns...),
		DefaultSnssai: sub.DefaultSnssai,
	}, nil
}

// DeregisterAMF3GPPAccess removes AMF registration for a SUPI.
func (r *Registry) DeregisterAMF3GPPAccess(supi string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.registrations, normalizeSUPI(supi))
}

// IsDnnAllowed checks subscriber profile for PDU session DNN.
func (r *Registry) IsDnnAllowed(supi, dnn string) bool {
	sub, ok := r.GetSubscriber(supi)
	if !ok {
		return false
	}
	dnn = strings.TrimSpace(strings.ToLower(dnn))
	for _, allowed := range sub.AllowedDnns {
		if strings.EqualFold(strings.TrimSpace(allowed), dnn) {
			return true
		}
	}
	return false
}
