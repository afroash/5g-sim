// Package sctp provides SCTP transport for NGAP messaging.
// NGAP (TS 38.413) runs over SCTP (TS 38.412).
//
// The AMF listens on port 38412 for incoming gNB SCTP associations.
// Each accepted connection represents one gNB connecting to the core.
//
// Ref: TS 38.412 §5 — Signalling transport
package sctp

import (
	"fmt"
	"net"

	"github.com/ishidawataru/sctp"
)

const (
	// NGAPPort is the well-known SCTP port for NGAP.
	// Ref: TS 38.412 §7
	NGAPPort = 38412

	// NGAPPPID is the Payload Protocol Identifier for NGAP.
	// Tells the network this SCTP stream carries NGAP data.
	// Ref: TS 38.412 §7 / IANA assignment
	NGAPPPID = 60

	// MaxMessageSize is the maximum NGAP message size we'll accept (32KB).
	MaxMessageSize = 32768
)

// Handler is a function called when a complete NGAP message is received
// from a connected gNB. The addr identifies which gNB sent it.
type Handler func(conn net.Conn, addr net.Addr, data []byte)

// Server listens for incoming SCTP connections from gNBs.
// In the 5G architecture this runs inside the AMF.
//
// Ref: TS 38.412 §5.1 — gNB initiates the SCTP association to the AMF.
type Server struct {
	port    int
	handler Handler
}

// NewServer creates a new SCTP server that will call handler for every
// NGAP message received from any connected gNB.
func NewServer(port int, handler Handler) *Server {
	return &Server{
		port:    port,
		handler: handler,
	}
}

// Listen starts the SCTP listener and blocks, accepting gNB connections.
// Each accepted connection is handled in its own goroutine so multiple
// gNBs can connect simultaneously.
func (s *Server) Listen() error {
	addr := &sctp.SCTPAddr{
		IPAddrs: []net.IPAddr{{IP: net.IPv4zero}},
		Port:    s.port,
	}

	ln, err := sctp.ListenSCTP("sctp", addr)
	if err != nil {
		return fmt.Errorf("sctp listen on port %d: %w", s.port, err)
	}
	defer ln.Close()

	fmt.Printf("[SCTP] Server listening on port %d (NGAP)\n", s.port)

	for {
		conn, err := ln.AcceptSCTP()
		if err != nil {
			return fmt.Errorf("sctp accept: %w", err)
		}

		fmt.Printf("[SCTP] New gNB connection from %s\n", conn.RemoteAddr())

		// Set SCTP options — inform the stack this is NGAP traffic.
		// Each gNB gets its own goroutine so we don't block other associations.
		go s.handleConn(conn)
	}
}

// handleConn reads NGAP messages from a single gNB connection in a loop.
// It reads raw bytes and passes them up to the registered Handler.
// If the connection drops, the goroutine exits cleanly.
func (s *Server) handleConn(conn *sctp.SCTPConn) {
	defer func() {
		conn.Close()
		fmt.Printf("[SCTP] Connection closed: %s\n", conn.RemoteAddr())
	}()

	buf := make([]byte, MaxMessageSize)

	for {
		// ReadMsg returns the raw SCTP message with its PPID.
		// We pass the raw bytes up to the NGAP handler.
		n, info, err := conn.SCTPRead(buf)
		if err != nil {
			fmt.Printf("[SCTP] Read error from %s: %v\n", conn.RemoteAddr(), err)
			return
		}

		if info != nil && info.PPID != NGAPPPID {
			fmt.Printf("[SCTP] Unexpected PPID %d from %s, expected %d\n",
				info.PPID, conn.RemoteAddr(), NGAPPPID)
			continue
		}

		// Copy the data out of the shared buffer before handing off.
		msg := make([]byte, n)
		copy(msg, buf[:n])

		fmt.Printf("[SCTP] Received %d bytes from %s\n", n, conn.RemoteAddr())

		// Hand off to the NGAP handler (non-blocking — handler runs in this goroutine).
		// If you need concurrent handling per-connection, wrap in go s.handler(...)
		s.handler(conn, conn.RemoteAddr(), msg)
	}
}

// Send writes a raw NGAP message to a connected gNB over SCTP.
// Sets the NGAP PPID so the remote side knows what protocol this is.
//
// Ref: TS 38.412 §7
func Send(conn *sctp.SCTPConn, data []byte) error {
	info := &sctp.SndRcvInfo{
		PPID:   NGAPPPID,
		Stream: 0,
	}

	_, err := conn.SCTPWrite(data, info)
	if err != nil {
		return fmt.Errorf("sctp write: %w", err)
	}

	return nil
}
