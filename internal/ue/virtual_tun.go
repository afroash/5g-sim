// virtual_tun.go — userspace packet I/O when kernel TUN is unavailable (standalone mode).
package ue

import "sync"

// virtualTUN is an in-process TUN substitute: uplink reads dequeue injected packets;
// downlink writes deliver packets from GTP toward the UE stack.
type virtualTUN struct {
	once   sync.Once
	uplink chan []byte
}

func newVirtualTUN() *virtualTUN {
	return &virtualTUN{uplink: make(chan []byte, 16)}
}

func (v *virtualTUN) Read(buf []byte) (int, error) {
	pkt, ok := <-v.uplink
	if !ok {
		return 0, errVirtualTUNClosed
	}
	n := copy(buf, pkt)
	return n, nil
}

func (v *virtualTUN) Write(pkt []byte) (int, error) {
	// Downlink from GTP is handled directly in the GTP handler (inject to virtual or log).
	return len(pkt), nil
}

// injectUplink queues a packet as if it arrived on the TUN (synthetic traffic tests).
func (v *virtualTUN) injectUplink(pkt []byte) {
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case v.uplink <- cp:
	default:
		// drop if congested
	}
}

func (v *virtualTUN) close() {
	v.once.Do(func() { close(v.uplink) })
}

type virtualTUNClosedError struct{}

func (virtualTUNClosedError) Error() string { return "virtual TUN closed" }

var errVirtualTUNClosed = virtualTUNClosedError{}
