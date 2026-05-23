// tun.go — TUN / userspace data plane and GTP-U user plane for the UE.
//
// Ref: TS 23.501 §5.8.2 — User plane architecture
// Ref: TS 29.281 — GTP-U
package ue

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"

	"github.com/afroash/5g-sim/internal/gtp"
	"github.com/songgao/water"
)

type packetIO interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}

// setupDataPlane brings up kernel TUN or userspace I/O plus GTP-U toward the gNB.
func (u *UE) setupDataPlane(allocatedIP string) error {
	mode := u.config.EffectiveDataPlaneMode()
	switch mode {
	case DataPlaneModeStandalone:
		return u.setupVirtualUserPlane(allocatedIP)
	case DataPlaneModeFabric:
		if err := u.setupKernelTUN(allocatedIP); err != nil {
			return err
		}
		u.userPlaneVirtual = false
		return nil
	default:
		if err := u.setupKernelTUN(allocatedIP); err != nil {
			fmt.Printf("[UE] kernel TUN unavailable (%v) — using userspace data plane\n", err)
			return u.setupVirtualUserPlane(allocatedIP)
		}
		u.userPlaneVirtual = false
		return nil
	}
}

func (u *UE) setupKernelTUN(allocatedIP string) error {
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
	if err := run("sysctl", "-w", "net.ipv6.conf."+iface.Name()+".disable_ipv6=1"); err != nil {
		fmt.Printf("[UE] IPv6 disable warning: %v\n", err)
	}
	if err := run("ip", "route", "add", "default", "dev", iface.Name()); err != nil {
		fmt.Printf("[UE] Default route warning: %v\n", err)
	}

	fmt.Printf("[UE] TUN interface %s up — IP %s/24\n", iface.Name(), allocatedIP)
	return u.startGTPUserPlane(iface)
}

func (u *UE) setupVirtualUserPlane(allocatedIP string) error {
	u.virtualTun = newVirtualTUN()
	u.userPlaneVirtual = true
	fmt.Printf("[UE] Userspace data plane active — IP %s (no kernel TUN)\n", allocatedIP)
	return u.startGTPUserPlane(u.virtualTun)
}

func (u *UE) startGTPUserPlane(io packetIO) error {
	u.icmpReplyCh = make(chan struct{}, 1)
	tunnel, err := gtp.NewTunnel(0)
	if err != nil {
		return fmt.Errorf("ue: GTP-U tunnel: %w", err)
	}
	u.tunnel = tunnel

	dlTEID := u.downlinkTEID
	if dlTEID == 0 {
		dlTEID = 1
	}

	tunnel.RegisterTEID(dlTEID, func(_ uint32, _ *net.UDPAddr, innerPkt []byte) {
		emitUEDownlinkObs(u.config.SUPI, dlTEID, innerPkt)
		dst := net.ParseIP(u.allocatedIP).To4()
		if isICMPEchoReply(innerPkt, dst) {
			select {
			case u.icmpReplyCh <- struct{}{}:
			default:
			}
		}
		if !u.userPlaneVirtual {
			if _, err := io.Write(innerPkt); err != nil {
				fmt.Printf("[UE] TUN write error: %v\n", err)
			}
		}
	})
	go tunnel.Serve()
	go u.packetUplinkLoop(io)
	return nil
}

func (u *UE) packetUplinkLoop(io packetIO) {
	buf := make([]byte, 65535)
	gnbGTPAddr, err := net.ResolveUDPAddr("udp4", u.config.GNBGTPAddress)
	if err != nil {
		fmt.Printf("[UE] Cannot resolve gNB GTP address %s: %v\n", u.config.GNBGTPAddress, err)
		return
	}

	u.mu.RLock()
	ueIP := net.ParseIP(u.allocatedIP).To4()
	u.mu.RUnlock()
	if ueIP == nil {
		fmt.Printf("[UE] Cannot parse allocated IP %q — uplink filter disabled\n", u.allocatedIP)
	}

	for {
		n, err := io.Read(buf)
		if err != nil {
			fmt.Printf("[UE] packet read error: %v\n", err)
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
		emitUEUplinkObs(u.allocatedIP, u.config.SUPI, ulTEID, pkt)
		if err := u.tunnel.SendGPDU(gnbGTPAddr, ulTEID, pkt); err != nil {
			fmt.Printf("[UE] GTP-U uplink error: %v\n", err)
		}
	}
}

func (u *UE) shouldForwardUplink(pkt []byte, ueIP net.IP) bool {
	if len(pkt) < 20 {
		return false
	}
	if pkt[0]>>4 != 4 {
		return false
	}
	if ueIP == nil {
		return true
	}
	return bytes.Equal(pkt[12:16], ueIP)
}

// setupTUN is kept as an alias for callers/tests.
func (u *UE) setupTUN(allocatedIP string) error {
	return u.setupDataPlane(allocatedIP)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
