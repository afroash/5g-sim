// Package obs is the observability hub for 5g-sim.
//
// Every component calls obs.Event() when something happens. The hub
// fans out to all configured sinks simultaneously:
//
//	obs.Event(obs.NGSetupRequest, "gNB", "AMF", ngapBytes)
//	  ├── pcap writer  → ngap.pcap  (Wireshark-readable)
//	  ├── seq recorder → procedure.mmd (Mermaid diagram)
//	  └── logger       → console + sim.jsonl
//
// Usage:
//
//	// At startup
//	hub := obs.NewHub("./captures")
//	defer hub.Close()
//
//	// In each component
//	hub.NGAP("gNB", "AMF", ngapPayload)
//	hub.GTPU("gNB", "UPF", gtpuPayload)
//	hub.Procedure("AMF", obs.EvRegistrationAccept, "UE registered", "TS 23.502 §4.2.2")
package obs

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/afroash/5g-sim/pkg/obslog"
	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/pcap"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

// Hub is the central observability coordinator.
// One Hub is created per process and shared across components.
type Hub struct {
	dir      string
	ngapPCAP *pcap.Writer
	gtpPCAP  *pcap.Writer
	seq      *seqdiag.Recorder
	log      *obslog.Logger
	started  time.Time
}

// NewHub creates a Hub that writes captures to the given directory.
// The directory is created if it does not exist.
func NewHub(dir string) (*Hub, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create capture dir %s: %w", dir, err)
	}

	// NGAP capture (SCTP frames, Linux SLL link type)
	ngapPath := dir + "/ngap.pcap"
	ngapW, err := pcap.NewWriter(ngapPath, pcap.LinkTypeLinuxSLL)
	if err != nil {
		return nil, fmt.Errorf("NGAP pcap: %w", err)
	}

	// GTP-U capture (UDP frames, Ethernet link type)
	gtpPath := dir + "/gtpu.pcap"
	gtpW, err := pcap.NewWriter(gtpPath, pcap.LinkTypeEthernet)
	if err != nil {
		return nil, fmt.Errorf("GTP-U pcap: %w", err)
	}

	// JSON structured log
	if err := obslog.InitFile(dir + "/sim.jsonl"); err != nil {
		return nil, fmt.Errorf("log file: %w", err)
	}

	fmt.Printf("[obs] Capture directory: %s\n", dir)
	fmt.Printf("[obs] NGAP pcap:         %s\n", ngapPath)
	fmt.Printf("[obs] GTP-U pcap:        %s\n", gtpPath)
	fmt.Printf("[obs] Structured log:    %s\n", dir+"/sim.jsonl")

	return &Hub{
		dir:      dir,
		ngapPCAP: ngapW,
		gtpPCAP:  gtpW,
		seq:      seqdiag.NewRecorder(),
		log:      obslog.New("obs"),
		started:  time.Now(),
	}, nil
}

// --- NGAP captures ---

// NGAP records an NGAP message exchange.
// payload is the raw NGAP PDU bytes.
func (h *Hub) NGAP(from, to string, payload []byte) {
	src := net.ParseIP("127.0.0.1")
	dst := net.ParseIP("127.0.0.1")

	frame := pcap.BuildSCTPFrame(src, dst, 54321, 38412, payload)
	if err := h.ngapPCAP.WritePacket(frame); err != nil {
		fmt.Printf("[obs] NGAP pcap write error: %v\n", err)
	}
}

// --- GTP-U captures ---

// GTPU records a GTP-U packet.
// payload is the raw GTP-U frame bytes (header + inner IP).
func (h *Hub) GTPU(from, to string, payload []byte) {
	src := net.ParseIP("127.0.0.1")
	dst := net.ParseIP("127.0.0.1")

	frame := pcap.BuildUDPFrame(src, dst, 2152, 2152, payload)
	if err := h.gtpPCAP.WritePacket(frame); err != nil {
		fmt.Printf("[obs] GTP-U pcap write error: %v\n", err)
	}
}

// MakeCaptureFunc returns a gtp.CaptureFunc that writes packets to the GTP-U pcap.
// Attach this to a gtp.Tunnel after creation:
//
//	tunnel.Capture = hub.MakeCaptureFunc("gNB", "UPF")
func (h *Hub) MakeCaptureFunc(from, to string) func(direction string, data []byte) {
	return func(direction string, data []byte) {
		// Build a UDP/IP frame so Wireshark can dissect it
		src := net.ParseIP("127.0.0.1")
		dst := net.ParseIP("127.0.0.1")
		frame := pcap.BuildUDPFrame(src, dst, 2152, 2152, data)
		if err := h.gtpPCAP.WritePacket(frame); err != nil {
			fmt.Printf("[obs] GTP-U pcap write error: %v\n", err)
		}
	}
}

// --- Procedure events (sequence diagram + log) ---

// Procedure records a 5G procedure event for the sequence diagram and log.
func (h *Hub) Procedure(from, to seqdiag.Node, label, specRef string, kvpairs ...string) {
	h.ProcedureWithDetail(from, to, label, label, specRef, kvpairs...)
}

// ProcedureWithDetail records a procedure step with separate GUI type and detail strings.
func (h *Hub) ProcedureWithDetail(from, to seqdiag.Node, typ, detail, specRef string, kvpairs ...string) {
	h.seq.Message(from, to, typ, specRef)

	fields := make(map[string]string)
	for i := 0; i+1 < len(kvpairs); i += 2 {
		fields[kvpairs[i]] = kvpairs[i+1]
	}
	if obspub.Enabled() {
		obspub.ProcedureWithDetail(from, to, typ, detail, specRef, fields)
	}

	obslog.New(string(from)).Info(
		fmt.Sprintf("→ %s: %s", string(to), typ),
		specRef,
		kvpairs...,
	)
}

// Note records an annotation over nodes in the sequence diagram.
func (h *Hub) Note(text string, nodes ...seqdiag.Node) {
	h.seq.Note(text, nodes...)
}

// Separator adds a visual procedure boundary in the sequence diagram.
func (h *Hub) Separator(title string) {
	h.seq.Separator(title)
}

// Log returns a structured logger for the named component.
func (h *Hub) Log(component string) *obslog.Logger {
	return obslog.New(component)
}

// --- Output ---

// Flush writes all pending outputs to disk.
// Call at shutdown or at the end of a test.
func (h *Hub) Flush() {
	// Write sequence diagram files
	mmdPath := h.dir + "/procedure.mmd"
	htmlPath := h.dir + "/procedure.html"

	if err := h.seq.WriteFile(mmdPath); err != nil {
		fmt.Printf("[obs] seqdiag write error: %v\n", err)
	}
	if err := h.seq.WriteHTML(htmlPath); err != nil {
		fmt.Printf("[obs] seqdiag HTML error: %v\n", err)
	}

	fmt.Printf("[obs] Sequence diagram: open %s in a browser\n", htmlPath)
	fmt.Printf("[obs] NGAP capture:     wireshark %s/ngap.pcap\n", h.dir)
	fmt.Printf("[obs] GTP-U capture:    wireshark %s/gtpu.pcap\n", h.dir)
}

// Close flushes all sinks and closes file handles.
func (h *Hub) Close() {
	h.Flush()
	h.ngapPCAP.Close()
	h.gtpPCAP.Close()
	obslog.Close()
}
