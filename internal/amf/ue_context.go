// ue_context.go — UE context management in the AMF.
//
// When a UE registers, the AMF creates a UEContext to track everything
// it knows about that UE: its identity, assigned GUTI, allowed slices,
// security context, and which RAN it's connected through.
//
// Ref: TS 23.502 §4.2.2 — Registration procedure
// Ref: TS 29.518 §5.2.2 — AMF UE context
package amf

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/afroash/5g-sim/internal/nas"
)

// UEContext holds the AMF's view of a single registered UE.
// Created during Registration, updated during mobility and session procedures.
//
// Ref: TS 23.501 §5.9 — UE context in AMF
type UEContext struct {
	// SUPI is the permanent identity. Learned from SUCI decryption
	// (we skip decryption in simulation — treat SUCI as SUPI directly).
	// Ref: TS 23.003 §2.2B
	SUPI string

	// GUTI is the temporary identity assigned by this AMF.
	// Sent to UE in Registration Accept and used in all future signalling.
	// Ref: TS 23.003 §2.10
	GUTI *nas.GUTI5G

	// AMFUeNgapID is the AMF's local identifier for this UE on N2.
	// Used in all NGAP messages to identify which UE is being referred to.
	// Ref: TS 38.413 §9.3.3.1
	AMFUeNgapID int64

	// RAN is the gNB this UE is currently connected through.
	RAN *RAN

	// AllowedNSSAI is the set of slices this UE is allowed to use.
	AllowedNSSAI []nas.SNSSAI

	// RegistrationType is how the UE registered (initial/mobility/periodic).
	RegistrationType uint8

	// State tracks where in the registration procedure we are.
	State UEState

	// SMContextRef is the SMF context URI for this UE's PDU session.
	// Used to release the session on deregistration.
	SMContextRef string

	// AllocatedIP is the IP address assigned by the SMF.
	AllocatedIP string

	// UPFAddr is the UPF GTP-U endpoint ("ip:port") for this session.
	UPFAddr string

	// ULTEID is the TEID the gNB should use for uplink GTP-U packets.
	ULTEID uint32

	// PendingNASAccept holds the NAS PDU Session Establishment Accept waiting to be
	// sent after the gNB confirms N3 resources via PDUSessionResourceSetupResponse.
	// Ref: TS 23.502 §4.3.2.2.2 step 10
	PendingNASAccept []byte

	// PendingPDUSessionID is the session ID for the pending NAS Accept.
	PendingPDUSessionID uint8

	// RegisteredAt is when this UE completed registration.
	RegisteredAt time.Time
}

// UEState represents the UE's registration state in the AMF.
// Ref: TS 23.501 §5.1 — UE states
type UEState int

const (
	UEStateDeregistered UEState = iota // Not registered
	UEStateRegistering                 // Registration in progress
	UEStateRegistered                  // Fully registered
)

func (s UEState) String() string {
	switch s {
	case UEStateDeregistered:
		return "DEREGISTERED"
	case UEStateRegistering:
		return "REGISTERING"
	case UEStateRegistered:
		return "REGISTERED"
	default:
		return "UNKNOWN"
	}
}

// ueStore manages all UE contexts in the AMF.
// Indexed two ways: by SUPI (permanent) and by AMF UE NGAP ID (per-connection).
type ueStore struct {
	mu         sync.RWMutex
	byNgapID   map[int64]*UEContext
	bySUPI     map[string]*UEContext
	nextNgapID int64
}

func newUEStore() *ueStore {
	return &ueStore{
		byNgapID:   make(map[int64]*UEContext),
		bySUPI:     make(map[string]*UEContext),
		nextNgapID: 1,
	}
}

// Add stores a new UE context and assigns an AMF UE NGAP ID.
func (s *ueStore) Add(ue *UEContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ue.AMFUeNgapID = s.nextNgapID
	s.nextNgapID++
	s.byNgapID[ue.AMFUeNgapID] = ue
	if ue.SUPI != "" {
		s.bySUPI[ue.SUPI] = ue
	}
}

// GetByNgapID retrieves a UE context by its NGAP ID.
func (s *ueStore) GetByNgapID(id int64) (*UEContext, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ue, ok := s.byNgapID[id]
	return ue, ok
}

// GetBySUPI retrieves a UE context by SUPI.
func (s *ueStore) GetBySUPI(supi string) (*UEContext, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ue, ok := s.bySUPI[supi]
	return ue, ok
}

// Count returns the number of registered UEs.
func (s *ueStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byNgapID)
}

// AllocateGUTI generates a new 5G-GUTI for a UE.
// The TMSI is randomly generated to avoid predictability.
// Ref: TS 23.003 §2.10
func (a *AMF) AllocateGUTI() (*nas.GUTI5G, error) {
	var tmsiBytes [4]byte
	if _, err := rand.Read(tmsiBytes[:]); err != nil {
		return nil, fmt.Errorf("generate TMSI: %w", err)
	}

	return &nas.GUTI5G{
		PLMN:      a.config.PLMN,
		AMFRegion: a.config.RegionID,
		AMFSet:    a.config.SetID,
		AMFPtr:    a.config.Pointer,
		TMSI:      binary.BigEndian.Uint32(tmsiBytes[:]),
	}, nil
}
