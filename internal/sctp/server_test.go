// server_test.go — Tests for the SCTP transport layer.
//
// Note: Full SCTP association tests require Linux kernel SCTP support
// (lksctp-tools). These tests verify the package structure and basic
// loopback behaviour. Run on Linux with: go test ./internal/sctp/...
package sctp

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestClientServerLoopback tests that a message sent from a client
// is received by the server handler correctly.
//
// Requires: Linux with SCTP kernel module loaded (modprobe sctp)
func TestClientServerLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SCTP loopback test in short mode (requires kernel SCTP)")
	}

	testMsg := []byte("NGAP-test-payload")
	received := make(chan []byte, 1)
	var wg sync.WaitGroup

	// Start server
	srv := NewServer(38412, func(_ net.Conn, _ net.Addr, data []byte) {
		received <- data
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Server will block — we'll let it run briefly
		_ = srv.Listen()
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Connect client and send
	client, err := Connect("127.0.0.1", 38412, func(_ net.Conn, _ net.Addr, _ []byte) {})
	if err != nil {
		t.Skipf("SCTP not available on this system: %v", err)
	}
	defer client.Close()

	if err := client.Send(testMsg); err != nil {
		t.Fatalf("client send: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg) != string(testMsg) {
			t.Errorf("got %q, want %q", msg, testMsg)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout: server did not receive message")
	}
}

// TestConstants verifies the NGAP port and PPID match the spec.
// Ref: TS 38.412 §7
func TestConstants(t *testing.T) {
	if NGAPPort != 38412 {
		t.Errorf("NGAPPort = %d, want 38412 (TS 38.412 §7)", NGAPPort)
	}

	// PPID 60 = 0x3C in decimal, assigned for NGAP by IANA
	if NGAPPPID != 60 {
		t.Errorf("NGAPPPID = %d, want 60 (IANA SCTP PPID for NGAP)", NGAPPPID)
	}
}
