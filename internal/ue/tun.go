// tun.go — TUN interface setup and GTP-U user plane for the standalone UE.
//
// After PDU session establishment, creates a TUN interface, assigns the UE IP,
// installs a default route, and starts the GTP-U send/receive goroutines so
// that all traffic from the UE container flows through the 5G data path.
//
// Requires NET_ADMIN capability in the container.
//
// Ref: TS 23.501 §5.8.2 — User plane architecture
// Ref: TS 29.281 — GTP-U
package ue

import (
	"fmt"
	"net"
	"os/exec"

	"github.com/songgao/water"

	"github.com/afroash/5g-sim/internal/gtp"
)

// setupTUN creates a TUN interface named "ue0", assigns the UE IP (/24 prefix),
// installs a default route via the interface, and starts GTP-U goroutines.
// allocatedIP is dotted-decimal, e.g. "10.45.0.2".
// Ref: Linux TUN/TAP driver
func (u *UE) setupTUN(allocatedIP string) error {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = "ue0"
	iface, err := water.New(cfg)
	if err != nil {
		return fmt.Errorf("ue: create TUN: %w", err)
	}

	if err := run("ip", "addr", "add", allocatedIP+"/24", "dev", iface.Name()); err != nil {
		return fmt.Errorf("ue: assign IP to %s: %w", iface.Name(), err)
	}
	if err := run("ip", "link", "set", iface.Name(), "up"); err != nil {
		return fmt.Errorf("ue: bring up %s: %w", iface.Name(), err)
	}
	if err := run("ip", "route", "add", "default", "dev", iface.Name()); err != nil {
		// Non-fatal — route may already exist or overlap with existing routes.
		fmt.Printf("[UE] Default route warning: %v\n", err)
	}

	fmt.Printf("[UE] TUN interface %s up — IP %s/24\n", iface.Name(), allocatedIP)

	// Create the GTP-U socket for user plane traffic.
	// Port 0 = OS-assigned; the gNB learns our address from the first uplink packet.
	tunnel, err := gtp.NewTunnel(0)
	if err != nil {
		return fmt.Errorf("ue: GTP-U tunnel: %w", err)
	}
	u.tunnel = tunnel

	// Register handler for downlink GTP-U packets (TEID = PDU session ID = 1).
	// Injects decapsulated IP packets back into the TUN.
	// Ref: TS 29.281 §5.1
	tunnel.RegisterTEID(1, func(_ uint32, _ *net.UDPAddr, innerPkt []byte) {
		if _, err := iface.Write(innerPkt); err != nil {
			fmt.Printf("[UE] TUN write error: %v\n", err)
		}
	})
	go tunnel.Serve()

	// Start uplink goroutine: reads from TUN, GTP-U encapsulates, sends to gNB.
	go u.tunUplinkLoop(iface)

	return nil
}

// tunUplinkLoop reads IP packets from the TUN interface and GTP-U encapsulates
// them for transmission to the gNB's UE-facing GTP port.
// Uplink TEID = 1 (PDU session 1); the gNB maps this to the UPF UL TEID.
// Ref: TS 29.281 §5.1 — G-PDU
func (u *UE) tunUplinkLoop(iface *water.Interface) {
	buf := make([]byte, 65535)
	gnbGTPAddr, err := net.ResolveUDPAddr("udp4", u.config.GNBGTPAddress)
	if err != nil {
		fmt.Printf("[UE] Cannot resolve gNB GTP address %s: %v\n", u.config.GNBGTPAddress, err)
		return
	}

	for {
		n, err := iface.Read(buf)
		if err != nil {
			fmt.Printf("[UE] TUN read error: %v\n", err)
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])

		if err := u.tunnel.SendGPDU(gnbGTPAddr, 1, pkt); err != nil {
			fmt.Printf("[UE] GTP-U uplink error: %v\n", err)
		}
	}
}

// run executes a shell command and returns an error if it fails.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
