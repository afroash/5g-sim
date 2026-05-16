// Package seqdiag generates Mermaid sequence diagrams from 5G procedure events.
//
// As messages flow through the simulator each component emits events.
// This package collects them and renders a Mermaid sequenceDiagram that
// shows the exact inter-node message flow with timestamps and TS references.
//
// Example output (5G registration + PDU session):
//
//	sequenceDiagram
//	  participant UE
//	  participant gNB
//	  participant AMF
//	  participant SMF
//	  participant UPF
//
//	  Note over gNB,AMF: NG Setup [TS 38.413 §8.7.1]
//	  gNB->>AMF: NGSetupRequest
//	  AMF->>gNB: NGSetupResponse
//	  ...
//
// Ref: https://mermaid.js.org/syntax/sequenceDiagram.html
package seqdiag

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Node represents a network function in the sequence diagram.
type Node string

const (
	NodeUE  Node = "UE"
	NodeGNB Node = "gNB"
	NodeAMF Node = "AMF"
	NodeSMF Node = "SMF"
	NodeUPF Node = "UPF"
	NodeNRF Node = "NRF"
	NodeUDM Node = "UDM"
)

// EventKind classifies the type of sequence diagram entry.
type EventKind int

const (
	KindMessage   EventKind = iota // Arrow between two nodes
	KindNote                       // Note over one or more nodes
	KindSeparator                  // Visual break between procedures
)

// Event is one entry in the procedure trace.
type Event struct {
	Kind      EventKind
	Timestamp time.Time

	// For KindMessage
	From    Node
	To      Node
	Label   string // Message name
	SpecRef string // e.g. "TS 38.413 §9.2.5.1"

	// For KindNote
	Nodes []Node
	Note  string

	// For KindSeparator
	Title string
}

// Recorder collects procedure events and renders them as Mermaid diagrams.
// Safe for concurrent use.
type Recorder struct {
	mu     sync.Mutex
	events []Event
	start  time.Time
}

// NewRecorder creates a new empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{start: time.Now()}
}

// Message records a message passing between two nodes.
// specRef is the 3GPP spec reference e.g. "TS 38.413 §9.2.5.1".
func (r *Recorder) Message(from, to Node, label, specRef string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{
		Kind:      KindMessage,
		Timestamp: time.Now(),
		From:      from,
		To:        to,
		Label:     label,
		SpecRef:   specRef,
	})
}

// Note records a note over one or more nodes.
func (r *Recorder) Note(note string, nodes ...Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{
		Kind:      KindNote,
		Timestamp: time.Now(),
		Nodes:     nodes,
		Note:      note,
	})
}

// Separator records a labelled visual break between procedures.
func (r *Recorder) Separator(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{
		Kind:      KindSeparator,
		Timestamp: time.Now(),
		Title:     title,
	})
}

// Render generates a Mermaid sequenceDiagram string from recorded events.
func (r *Recorder) Render() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var sb strings.Builder

	sb.WriteString("sequenceDiagram\n")
	sb.WriteString("  autonumber\n")
	sb.WriteString("  participant UE\n")
	sb.WriteString("  participant gNB\n")
	sb.WriteString("  participant AMF\n")
	sb.WriteString("  participant SMF\n")
	sb.WriteString("  participant UPF\n")
	sb.WriteString("  participant NRF\n")
	sb.WriteString("\n")

	for _, ev := range r.events {
		elapsed := ev.Timestamp.Sub(r.start).Milliseconds()

		switch ev.Kind {
		case KindMessage:
			// Arrow line
			label := ev.Label
			if ev.SpecRef != "" {
				label = fmt.Sprintf("%s<br/><small>[%s]</small>", ev.Label, ev.SpecRef)
			}
			sb.WriteString(fmt.Sprintf("  %s->%s: %s (+%dms)\n",
				string(ev.From), string(ev.To), label, elapsed))

		case KindNote:
			nodeStrs := make([]string, len(ev.Nodes))
			for i, n := range ev.Nodes {
				nodeStrs[i] = string(n)
			}
			if len(nodeStrs) == 1 {
				sb.WriteString(fmt.Sprintf("  Note over %s: %s\n",
					nodeStrs[0], ev.Note))
			} else {
				sb.WriteString(fmt.Sprintf("  Note over %s: %s\n",
					strings.Join(nodeStrs, ","), ev.Note))
			}

		case KindSeparator:
			sb.WriteString(fmt.Sprintf("\n  rect rgb(240, 240, 240)\n"))
			sb.WriteString(fmt.Sprintf("    Note over UE,NRF: %s\n", ev.Title))
			sb.WriteString("  end\n\n")
		}
	}

	return sb.String()
}

// WriteFile writes the Mermaid diagram to a .mmd file.
func (r *Recorder) WriteFile(path string) error {
	content := r.Render()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write diagram %s: %w", path, err)
	}
	fmt.Printf("[SeqDiag] Written to %s (%d events)\n", path, r.EventCount())
	return nil
}

// EventCount returns the number of recorded events.
func (r *Recorder) EventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// WriteHTML writes a self-contained HTML file that renders the diagram
// in a browser using the Mermaid JS CDN — no install required.
func (r *Recorder) WriteHTML(path string) error {
	mmd := r.Render()

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>5g-sim — Procedure Sequence Diagram</title>
  <script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
  <style>
    body { font-family: monospace; padding: 2em; background: #fafafa; }
    h1 { color: #333; }
    .mermaid { background: white; padding: 2em; border-radius: 8px;
               box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    .meta { color: #888; font-size: 0.85em; margin-bottom: 1em; }
  </style>
</head>
<body>
  <h1>5g-sim — 5G Procedure Sequence Diagram</h1>
  <p class="meta">Generated by 5g-sim observability layer</p>
  <div class="mermaid">
%s
  </div>
  <script>mermaid.initialize({startOnLoad:true, theme:'default'});</script>
</body>
</html>`, mmd)

	if err := os.WriteFile(path, []byte(html), 0644); err != nil {
		return fmt.Errorf("write HTML %s: %w", path, err)
	}
	fmt.Printf("[SeqDiag] HTML written to %s\n", path)
	return nil
}
