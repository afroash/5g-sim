// connectivity.go — Startup connectivity test for the standalone UE.
//
// After the TUN interface is up, verifies the full data path:
//
//	UE TUN → GTP-U → gNB → UPF → internet-sim
//
// Ref: RFC 792 (ICMP), RFC 7230 (HTTP/1.1)
package ue

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const (
	internetSimAddr = "10.100.0.1"
	pingCount       = 4
	httpTimeout     = 5 * time.Second
)

// runConnectivityTest executes an ICMP ping and HTTP GET to internet-sim,
// then prints a PASS/FAIL summary.
func (u *UE) runConnectivityTest() {
	fmt.Println("[UE] ─── Connectivity Test ───────────────────────")

	pingOK := pingTest(internetSimAddr, pingCount)
	httpOK := httpTest(fmt.Sprintf("http://%s/", internetSimAddr))

	fmt.Println("[UE] ─────────────────────────────────────────────")
	if pingOK && httpOK {
		fmt.Println("[UE] ✓ PASS — user plane fully operational")
	} else {
		fmt.Println("[UE] ✗ FAIL — one or more connectivity checks failed")
	}
}

// pingTest sends count ICMP echo requests to dst via the system ping binary.
// Traffic flows through ue0 and the GTP-U tunnel.
// Returns true if at least one reply was received.
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

// httpTest makes a GET request to url and logs the status code and body preview.
// Returns true if the response status is 2xx.
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
