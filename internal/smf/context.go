// context.go — SMF session store and IP address pool.
//
// The SMF needs two things:
//  1. An IP pool to allocate addresses from when UEs create sessions
//  2. A session store to track active PDU sessions
//
// Both are in-memory — sufficient for simulation.
//
// Ref: TS 23.502 §4.3.2 — PDU Session Establishment
package smf

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

// IPPool manages a pool of IPv4 addresses for UE allocation.
// Addresses are allocated sequentially from a CIDR range.
//
// Ref: TS 23.501 §5.8.2 — PDU Session IP address allocation
type IPPool struct {
	mu        sync.Mutex
	network   *net.IPNet
	next      uint32            // next address to allocate (as uint32)
	last      uint32            // last usable address
	allocated map[string]string // ip → SUPI
}

// NewIPPool creates an IP pool from a CIDR string.
// e.g. NewIPPool("10.0.0.0/24") gives addresses 10.0.0.1–10.0.0.254
func NewIPPool(cidr string) (*IPPool, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	// First usable = network address + 1
	base := binary.BigEndian.Uint32(network.IP.To4())

	// Last usable = broadcast - 1
	mask := binary.BigEndian.Uint32(network.Mask)
	broadcast := base | ^mask

	return &IPPool{
		network:   network,
		next:      base + 1,
		last:      broadcast - 1,
		allocated: make(map[string]string),
	}, nil
}

// Allocate assigns the next available IP address to a SUPI.
// Returns the IP as a string (e.g. "10.0.0.1") or error if pool exhausted.
func (p *IPPool) Allocate(supi string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.next > p.last {
		return "", fmt.Errorf("IP pool exhausted")
	}

	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, p.next)
	p.next++

	ipStr := ip.String()
	p.allocated[ipStr] = supi

	fmt.Printf("[SMF] Allocated IP %s to SUPI %s\n", ipStr, supi)
	return ipStr, nil
}

// Release returns an IP address to the pool.
func (p *IPPool) Release(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.allocated, ip)
	fmt.Printf("[SMF] Released IP %s\n", ip)
}

// Count returns the number of allocated addresses.
func (p *IPPool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.allocated)
}

// SessionStore manages active PDU session contexts.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SmContext // key: context ID
	counter  int
}

// NewSessionStore creates an empty session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*SmContext),
	}
}

// Add stores a new session context and returns its assigned ID.
func (s *SessionStore) Add(ctx *SmContext) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	ctx.ID = fmt.Sprintf("ctx-%05d", s.counter)
	s.sessions[ctx.ID] = ctx
	fmt.Printf("[SMF] Session created: id=%s supi=%s ip=%s dnn=%s\n",
		ctx.ID, ctx.SUPI, ctx.AllocatedIP, ctx.DNN)
	return ctx.ID
}

// Get retrieves a session by its context ID.
func (s *SessionStore) Get(id string) (*SmContext, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, ok := s.sessions[id]
	return ctx, ok
}

// Delete removes a session context.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Count returns the number of active sessions.
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}
