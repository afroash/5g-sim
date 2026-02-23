// teid.go — TEID allocation for the SMF.
//
// When a PDU session is established, the UPF needs two TEIDs:
//   - UL TEID: allocated by UPF, used by gNB when sending uplink traffic
//   - DL TEID: allocated by gNB, used by UPF when sending downlink traffic
//
// The SMF coordinates this during session setup.
//
// Ref: TS 23.502 §4.3.2.2.1 — PDU Session Establishment
// Ref: TS 29.281 §5.1 — TEID assignment
package smf

import (
	"fmt"
	"sync/atomic"
)

// teidCounter is a process-wide TEID counter.
// In production each UPF would manage its own TEID space.
var teidCounter uint32 = 0

// AllocateTEID assigns the next available TEID.
// TEIDs are 32-bit values; we allocate sequentially from 1.
// Ref: TS 29.281 §5.1
func AllocateTEID() uint32 {
	return atomic.AddUint32(&teidCounter, 1)
}

// GTPTunnel holds the GTP-U tunnel parameters for a PDU session.
// Exchanged between the SMF/UPF and gNB during session setup.
// Ref: TS 29.502 §6.1.6.2.10 — TunnelInfo
type GTPTunnel struct {
	// UPFAddress is the IP:port of the UPF's GTP-U endpoint.
	UPFAddress string `json:"upfAddress"`

	// ULTEID is the TEID the gNB must use when sending uplink packets.
	// Allocated by the UPF, given to the gNB via the AMF.
	ULTEID uint32 `json:"ulTeid"`
}

// String formats tunnel info for logging.
func (t *GTPTunnel) String() string {
	return fmt.Sprintf("UPF=%s UL-TEID=0x%08X", t.UPFAddress, t.ULTEID)
}
