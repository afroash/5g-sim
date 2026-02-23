// client.go — SCTP client used by the gNB simulator to connect to the AMF.
//
// In 5G, the gNB always initiates the SCTP association to the AMF.
// The AMF never connects outward — it only listens.
//
// Ref: TS 38.412 §5.1 — The gNB shall establish the SCTP association.
package sctp

import (
	"fmt"
	"net"

	"github.com/ishidawataru/sctp"
)

// Client represents a gNB's SCTP connection to the AMF.
// Once connected, it can send NGAP messages and receive responses.
type Client struct {
	conn    *sctp.SCTPConn
	handler Handler
}

// Connect establishes an SCTP association from the gNB to the AMF.
// host is the AMF's IP address or hostname. port is typically 38412.
//
// Ref: TS 38.412 §5.1
func Connect(host string, port int, handler Handler) (*Client, error) {
	addr := &sctp.SCTPAddr{
		IPAddrs: []net.IPAddr{{IP: net.ParseIP(host)}},
		Port:    port,
	}

	conn, err := sctp.DialSCTP("sctp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("sctp connect to %s:%d: %w", host, port, err)
	}

	fmt.Printf("[SCTP] Connected to AMF at %s:%d\n", host, port)

	c := &Client{
		conn:    conn,
		handler: handler,
	}

	// Start reading responses from AMF in the background.
	go c.readLoop()

	return c, nil
}

// Send writes a raw NGAP message to the AMF.
func (c *Client) Send(data []byte) error {
	return Send(c.conn, data)
}

// Close terminates the SCTP association.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Conn returns the underlying SCTP connection (for use with sctp.Send helper).
func (c *Client) Conn() *sctp.SCTPConn {
	return c.conn
}

// readLoop continuously reads NGAP messages from the AMF and calls the handler.
// Runs in its own goroutine from the moment Connect() succeeds.
func (c *Client) readLoop() {
	buf := make([]byte, MaxMessageSize)

	for {
		n, info, err := c.conn.SCTPRead(buf)
		if err != nil {
			fmt.Printf("[SCTP] AMF connection closed: %v\n", err)
			return
		}

		if info != nil && info.PPID != NGAPPPID {
			fmt.Printf("[SCTP] Unexpected PPID %d, expected %d\n", info.PPID, NGAPPPID)
			continue
		}

		msg := make([]byte, n)
		copy(msg, buf[:n])

		fmt.Printf("[SCTP] Received %d bytes from AMF\n", n)

		c.handler(c.conn, c.conn.RemoteAddr(), msg)
	}
}
