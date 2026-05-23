// connectivity.go — Post-attach connectivity verification.
package ue

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

)

const (
	pingCount   = 4
	httpTimeout = 5 * time.Second
)

func (u *UE) runConnectivityTest() {
	target := u.config.ConnectivityTarget()
	fmt.Println("[UE] ─── Connectivity Test ───────────────────────")

	var pingOK, httpOK bool
	if u.userPlaneVirtual {
		pingOK = u.runSyntheticPing(target)
		fmt.Printf("[UE]   userspace data plane — HTTP skipped (use fabric/clab for full N6)\n")
		httpOK = true
	} else {
		pingOK = pingTest(target, pingCount)
		httpOK = httpTest(fmt.Sprintf("http://%s/", target))
	}

	fmt.Println("[UE] ─────────────────────────────────────────────")
	if pingOK && httpOK {
		fmt.Println("[UE] ✓ PASS — user plane path exercised")
	} else {
		fmt.Println("[UE] ✗ FAIL — one or more connectivity checks failed")
	}
}

func (u *UE) runSyntheticPing(dst string) bool {
	fmt.Printf("[UE] synthetic ping %s (via GTP-U)...\n", dst)
	u.icmpReplyCh = make(chan struct{}, 1)

	pkt, err := buildICMPEchoRequest(u.allocatedIP, dst, 1, 1)
	if err != nil {
		fmt.Printf("[UE]   synthetic ping FAIL: %v\n", err)
		return false
	}

	gnbGTPAddr, err := net.ResolveUDPAddr("udp4", u.config.GNBGTPAddress)
	if err != nil {
		fmt.Printf("[UE]   synthetic ping FAIL: %v\n", err)
		return false
	}
	ulTEID := u.uplinkTEID
	if ulTEID == 0 {
		ulTEID = 1
	}
	emitUEUplinkObs(u.allocatedIP, u.config.SUPI, ulTEID, pkt)
	if err := u.tunnel.SendGPDU(gnbGTPAddr, ulTEID, pkt); err != nil {
		fmt.Printf("[UE]   synthetic ping FAIL: %v\n", err)
		return false
	}

	select {
	case <-u.icmpReplyCh:
		fmt.Printf("[UE]   synthetic ping PASS ✓ (ICMP echo reply via 5G user plane)\n")
		return true
	case <-time.After(8 * time.Second):
		fmt.Printf("[UE]   synthetic ping FAIL: timeout (UPF may be down or N6 not replying)\n")
		return false
	}
}

func pingTest(dst string, count int) bool {
	fmt.Printf("[UE] ping %s (%d packets)...\n", dst, count)
	cmd := exec.Command("ping", "-c", fmt.Sprintf("%d", count), "-W", "2", dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[UE]   ping FAIL: %v\n", err)
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "rtt") || strings.Contains(line, "round-trip") {
			fmt.Printf("[UE]   %s\n", strings.TrimSpace(line))
		}
	}
	fmt.Printf("[UE]   ping PASS ✓\n")
	return true
}

func httpTest(url string) bool {
	fmt.Printf("[UE] GET %s...\n", url)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("[UE]   HTTP FAIL: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	preview := strings.TrimSpace(string(body))
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	fmt.Printf("[UE]   HTTP %d — %s\n", resp.StatusCode, preview)

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if ok {
		fmt.Printf("[UE]   HTTP PASS ✓\n")
	} else {
		fmt.Printf("[UE]   HTTP FAIL ✗\n")
	}
	return ok
}
