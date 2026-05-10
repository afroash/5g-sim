// n6.go — N6 (UPF ↔ Data Network) forwarding via a TUN interface.
//
// In a real 5G deployment the UPF terminates GTP-U on N3 (toward the gNB)
// and exposes the user packets to the data network on N6. We model N6 as
// a Linux TUN device:
//
//	UE → gNB → (GTP-U) → UPF.tunnel(:2152) → decap → write(upf-n6 TUN)
//	  → kernel routes out via OSPF default route → internet-sim
//
//	internet-sim → kernel → routes 10.45.0.0/24 → upf-n6 → read TUN
//	  → UPF looks up session by inner dst IP → encap → send to gNB DL-TEID
//
// The TUN is given an IP inside the UE pool (e.g. 10.45.0.1/24) so the
// kernel auto-installs a connected route for the pool toward the TUN, and
// FRR's existing `network 10.45.0.0/24 area 0` directive picks it up and
// advertises it via OSPF — internet-sim's reply traffic finds its way back
// to server-b without any additional plumbing.
//
// Requires NET_ADMIN. Without it, StartN6 returns an error and the UPF
// falls back to its legacy "fake ICMP echo reply" mode (used by tests).
//
// Ref: TS 23.501 §5.8.2.11.3 — N6 reference point
// Ref: TS 29.281 §5.1         — G-PDU forwarding (the reverse direction)
package upf

import (
	"fmt"
	"os/exec"

	"github.com/songgao/water"
)

// N6 is the UPF's TUN-based data-network interface.
type N6 struct {
	iface *water.Interface
	name  string
}

// StartN6 creates and configures the UPF's N6 TUN interface.
//
// ifaceName is the kernel device name (e.g. "upf-n6").
// gatewayCIDR is the interface IP and prefix in CIDR form (e.g. "10.45.0.1/24");
// the prefix length determines which packets the host kernel will route onto
// this TUN, so it should cover the entire UE address pool.
//
// Returns a started N6 ready for Inject/ReadLoop. The caller is responsible
// for calling Close on shutdown.
//
// Ref: TS 23.501 §5.8.2.11.3 — N6
func StartN6(ifaceName, gatewayCIDR string) (*N6, error) {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = ifaceName
	iface, err := water.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("upf: n6: create TUN %s: %w", ifaceName, err)
	}

	if err := runCmd("ip", "addr", "add", gatewayCIDR, "dev", iface.Name()); err != nil {
		// Address may already be assigned from a previous run — non-fatal.
		fmt.Printf("[UPF] N6 addr add warning: %v\n", err)
	}
	if err := runCmd("ip", "link", "set", iface.Name(), "up"); err != nil {
		iface.Close()
		return nil, fmt.Errorf("upf: n6: bring up %s: %w", ifaceName, err)
	}

	fmt.Printf("[UPF] N6 TUN %s up — gateway %s\n", iface.Name(), gatewayCIDR)
	return &N6{iface: iface, name: iface.Name()}, nil
}

// Inject writes a raw IPv4 packet to the TUN. The kernel sees this as an
// ingress packet on the upf-n6 interface and routes it onward via the
// host's normal forwarding table.
//
// Ref: TS 23.501 §5.8.2.11.3 — N6 (UPF to Data Network direction)
func (n *N6) Inject(pkt []byte) error {
	if _, err := n.iface.Write(pkt); err != nil {
		return fmt.Errorf("upf: n6: write to %s: %w", n.name, err)
	}
	return nil
}

// ReadLoop blocks reading IP packets from the TUN and invoking handler
// for each one. Intended to be called in its own goroutine. Returns when
// the TUN is closed.
//
// Ref: TS 29.281 §5.1 — UPF builds a G-PDU per outbound packet
func (n *N6) ReadLoop(handler func(pkt []byte)) {
	buf := make([]byte, 65535)
	for {
		nbytes, err := n.iface.Read(buf)
		if err != nil {
			fmt.Printf("[UPF] N6 read loop exiting: %v\n", err)
			return
		}
		pkt := make([]byte, nbytes)
		copy(pkt, buf[:nbytes])
		handler(pkt)
	}
}

// Close shuts down the TUN. Subsequent reads/writes will fail.
func (n *N6) Close() error {
	if n.iface == nil {
		return nil
	}
	return n.iface.Close()
}

// runCmd executes a shell command and returns an error if it fails,
// including the combined output for easier diagnosis.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
