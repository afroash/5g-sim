// tunnel.go — GTP-U tunnel endpoint (UDP listener + TEID routing).
//
// A Tunnel is a UDP socket on port 2152 that:
//   - Receives GTP-U packets and dispatches them by TEID
//   - Sends GTP-U packets to a remote endpoint
//
// Used by both the gNB (sends UE traffic → UPF) and UPF
// (receives from gNB, decapsulates, forwards to internet).
//
// Ref: TS 29.281 §4.4 — GTP-U endpoints
package gtp

import (
	"fmt"
	"net"
	"sync"
)

// CaptureFunc is an optional callback for packet capture.
// If set, called with raw GTP-U bytes on every send/receive.
type CaptureFunc func(direction string, data []byte)

// HandlerFunc is called when a G-PDU arrives on a registered TEID.
// teid is the TEID from the packet header.
// src is the remote UDP address the packet came from.
// innerPkt is the decapsulated inner IP packet.
type HandlerFunc func(teid uint32, src *net.UDPAddr, innerPkt []byte)

type EchoHandlerFunc func(src *net.UDPAddr, seqNum uint16)

// Tunnel is a GTP-U UDP endpoint.
// It multiplexes incoming packets by TEID to registered handlers.
type Tunnel struct {
	conn            *net.UDPConn
	mu              sync.RWMutex
	handlers        map[uint32]HandlerFunc // TEID → handler
	defaultHandler  HandlerFunc            // used when TEID has no specific handler
	echo            EchoHandlerFunc
	nextTEID        uint32
	Capture         CaptureFunc // optional — set by obs hub
}

// NewTunnel creates and binds a GTP-U UDP socket on the given port.
// Use port=0 for an OS-assigned port (useful in tests).
// Ref: TS 29.281 §4.4.2 — UDP/IP based transport
func NewTunnel(port int) (*Tunnel, error) {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("bind GTP-U UDP port %d: %w", port, err)
	}

	t := &Tunnel{
		conn:     conn,
		handlers: make(map[uint32]HandlerFunc),
		nextTEID: 1,
	}

	fmt.Printf("[GTP-U] Tunnel listening on %s\n", conn.LocalAddr())
	return t, nil
}

// LocalAddr returns the local UDP address of this tunnel endpoint.
func (t *Tunnel) LocalAddr() *net.UDPAddr {
	return t.conn.LocalAddr().(*net.UDPAddr)
}

// AllocateTEID reserves and returns the next available TEID.
// TEIDs are assigned per-session per-direction.
// Ref: TS 29.281 §5.1 — TEID allocation
func (t *Tunnel) AllocateTEID() uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	teid := t.nextTEID
	t.nextTEID++
	return teid
}

// RegisterTEID registers a handler for incoming packets on a specific TEID.
// When a G-PDU arrives with this TEID, handler is called with the inner packet.
func (t *Tunnel) RegisterTEID(teid uint32, handler HandlerFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlers[teid] = handler
	fmt.Printf("[GTP-U] Registered TEID 0x%08X\n", teid)
}

// DeregisterTEID removes the handler for a TEID.
func (t *Tunnel) DeregisterTEID(teid uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.handlers, teid)
}

// RegisterDefaultHandler sets the fallback handler for G-PDUs with unregistered TEIDs.
func (t *Tunnel) RegisterDefaultHandler(handler HandlerFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.defaultHandler = handler
}

// SetEchoHandler registers a handler for Echo Requests.
// If not set, echo requests are answered automatically.
func (t *Tunnel) SetEchoHandler(h EchoHandlerFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.echo = h
}

// SendGPDU encapsulates an inner IP packet in GTP-U and sends it
// to the given remote address using the specified TEID.
// Ref: TS 29.281 §5.1
func (t *Tunnel) SendGPDU(remote *net.UDPAddr, teid uint32, innerPkt []byte) error {
	pkt := NewGPDU(teid, innerPkt)
	data, err := Encode(pkt)
	if err != nil {
		return fmt.Errorf("encode GTP-U: %w", err)
	}

	_, err = t.conn.WriteToUDP(data, remote)
	if err != nil {
		return fmt.Errorf("send GTP-U to %s: %w", remote, err)
	}
	if t.Capture != nil {
		t.Capture("tx", data)
	}
	return nil
}

// SendEchoRequest sends a GTP-U Echo Request for path verification.
// Ref: TS 29.281 §7.2.1
func (t *Tunnel) SendEchoRequest(remote *net.UDPAddr, seqNum uint16) error {
	pkt := NewEchoRequest(seqNum)
	data, err := Encode(pkt)
	if err != nil {
		return fmt.Errorf("encode echo request: %w", err)
	}
	_, err = t.conn.WriteToUDP(data, remote)
	return err
}

// Serve starts the receive loop. Blocks until the tunnel is closed.
// Dispatches incoming packets to registered TEID handlers.
func (t *Tunnel) Serve() {
	buf := make([]byte, 65535)
	for {
		n, src, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			// conn was closed — normal shutdown
			fmt.Printf("[GTP-U] Receive loop exiting: %v\n", err)
			return
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		if t.Capture != nil {
			t.Capture("rx", data)
		}
		go t.dispatch(src, data)
	}
}

// dispatch decodes a received GTP-U packet and routes it.
func (t *Tunnel) dispatch(src *net.UDPAddr, data []byte) {
	pkt, err := Decode(data)
	if err != nil {
		fmt.Printf("[GTP-U] Decode error from %s: %v\n", src, err)
		return
	}

	switch pkt.Header.MessageType {
	case MsgTypeGPDU:
		t.mu.RLock()
		handler, ok := t.handlers[pkt.Header.TEID]
		t.mu.RUnlock()

		if !ok {
			if t.defaultHandler != nil {
				t.defaultHandler(pkt.Header.TEID, src, pkt.Payload)
				return
			}
			fmt.Printf("[GTP-U] No handler for TEID 0x%08X (from %s)\n",
				pkt.Header.TEID, src)
			return
		}
		handler(pkt.Header.TEID, src, pkt.Payload)

	case MsgTypeEchoRequest:
		fmt.Printf("[GTP-U] Echo Request from %s seq=%d\n",
			src, pkt.Header.SequenceNumber)
		// Auto-respond unless a custom handler is set
		t.mu.RLock()
		echoHandler := t.echo
		t.mu.RUnlock()

		if echoHandler != nil {
			echoHandler(src, pkt.Header.SequenceNumber)
		} else {
			t.autoEchoResponse(src, pkt.Header.SequenceNumber)
		}

	case MsgTypeEchoResponse:
		fmt.Printf("[GTP-U] Echo Response from %s seq=%d\n",
			src, pkt.Header.SequenceNumber)

	case MsgTypeEndMarker:
		fmt.Printf("[GTP-U] End Marker from %s TEID=0x%08X\n",
			src, pkt.Header.TEID)

	default:
		fmt.Printf("[GTP-U] Unhandled message type 0x%02X from %s\n",
			pkt.Header.MessageType, src)
	}
}

// autoEchoResponse sends an Echo Response to an Echo Request.
func (t *Tunnel) autoEchoResponse(src *net.UDPAddr, seqNum uint16) {
	resp := NewEchoResponse(seqNum)
	data, err := Encode(resp)
	if err != nil {
		return
	}
	t.conn.WriteToUDP(data, src)
	fmt.Printf("[GTP-U] Echo Response sent to %s seq=%d\n", src, seqNum)
}

// Close shuts down the tunnel's UDP socket.
func (t *Tunnel) Close() error {
	return t.conn.Close()
}
