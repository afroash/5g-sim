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
	"bytes"
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
	tunName := u.config.TunName
	if tunName == "" {
		tunName = "ue0"
	}
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = tunName
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

	// Disable IPv6 on the TUN to stop the kernel auto-generating Router
	// Solicitations / DAD Neighbor Solicitations / Multicast Listener
	// Reports as soon as the link comes up. Those would otherwise be read
	// straight back out of the TUN and tunnelled toward the gNB, which
	// only forwards IPv4 in this simulator.
	if err := run("sysctl", "-w", "net.ipv6.conf."+iface.Name()+".disable_ipv6=1"); err != nil {
		fmt.Printf("[UE] IPv6 disable warning: %v\n", err)
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

	dlTEID := u.downlinkTEID
	if dlTEID == 0 {
		dlTEID = 1
	}
	// Register handler for downlink GTP-U packets (TEID from gNB via NAS accept).
	// Ref: TS 29.281 §5.1
	tunnel.RegisterTEID(dlTEID, func(_ uint32, _ *net.UDPAddr, innerPkt []byte) {
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
//
// Only IPv4 packets sourced from the UE's allocated IP are forwarded. This
// suppresses two classes of leakage that would otherwise corrupt the gNB's
// session bookkeeping:
//
//   - Kernel-generated IPv6 traffic on the TUN (RS, DAD NS, MLR).
//   - IPv4 packets the kernel happens to emit on the default route with a
//     source IP other than the UE IP (e.g. background container chatter
//     using eth0's mgmt address).
//
// Uplink TEID = 1 (PDU session 1); the gNB maps this to the UPF UL TEID.
// Ref: TS 29.281 §5.1 — G-PDU
func (u *UE) tunUplinkLoop(iface *water.Interface) {
	buf := make([]byte, 65535)
	gnbGTPAddr, err := net.ResolveUDPAddr("udp4", u.config.GNBGTPAddress)
	if err != nil {
		fmt.Printf("[UE] Cannot resolve gNB GTP address %s: %v\n", u.config.GNBGTPAddress, err)
		return
	}

	ueIP := net.ParseIP(u.allocatedIP).To4()
	if ueIP == nil {
		fmt.Printf("[UE] Cannot parse allocated IP %q — uplink filter disabled\n", u.allocatedIP)
	}

	for {
		n, err := iface.Read(buf)
		if err != nil {
			fmt.Printf("[UE] TUN read error: %v\n", err)
			return
		}
		if !u.shouldForwardUplink(buf[:n], ueIP) {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])

		ulTEID := u.uplinkTEID
		if ulTEID == 0 {
			ulTEID = 1
		}
		if err := u.tunnel.SendGPDU(gnbGTPAddr, ulTEID, pkt); err != nil {
			fmt.Printf("[UE] GTP-U uplink error: %v\n", err)
		}
	}
}

// shouldForwardUplink returns true iff pkt is an IPv4 packet whose source
// address matches the UE's allocated IP. Anything else is dropped at the
// TUN read loop so the gNB only ever sees clean UE-originated traffic.
//
// If ueIP is nil (parse failed during setup) the filter is bypassed and
// any IPv4 packet is forwarded — better than dropping everything.
func (u *UE) shouldForwardUplink(pkt []byte, ueIP net.IP) bool {
	if len(pkt) < 20 {
		return false
	}
	if pkt[0]>>4 != 4 {
		return false // not IPv4
	}
	if ueIP == nil {
		return true
	}
	return bytes.Equal(pkt[12:16], ueIP)
}

// run executes a shell command and returns an error if it fails.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
