// seqdiag_test.go — Tests for the sequence diagram recorder.
package seqdiag

import (
	"os"
	"strings"
	"testing"
)

func TestRecordAndRender(t *testing.T) {
	r := NewRecorder()

	r.Separator("NG Setup [TS 38.413 §8.7.1]")
	r.Message(NodeGNB, NodeAMF, "NGSetupRequest", "TS 38.413 §9.2.6.1")
	r.Message(NodeAMF, NodeGNB, "NGSetupResponse", "TS 38.413 §9.2.6.2")

	r.Separator("UE Registration [TS 23.502 §4.2.2]")
	r.Message(NodeGNB, NodeAMF, "InitialUEMessage", "TS 38.413 §9.2.5.1")
	r.Message(NodeAMF, NodeSMF, "Nsmf_PDUSession_CreateSMContext", "TS 29.502 §5.2.2.2")
	r.Message(NodeSMF, NodeAMF, "201 Created", "TS 29.502 §6.1.6.3.2")

	r.Note("UE IP allocated: 10.0.0.1", NodeAMF, NodeSMF)

	out := r.Render()

	if !strings.HasPrefix(out, "sequenceDiagram") {
		t.Error("output should start with 'sequenceDiagram'")
	}
	if !strings.Contains(out, "NGSetupRequest") {
		t.Error("output missing NGSetupRequest")
	}
	if !strings.Contains(out, "TS 38.413 §9.2.6.1") {
		t.Error("output missing spec reference")
	}
	if !strings.Contains(out, "participant gNB") {
		t.Error("output missing participant declaration")
	}
	if r.EventCount() != 8 {
		t.Errorf("EventCount = %d, want 8", r.EventCount())
	}

	t.Logf("Rendered diagram (%d chars, %d events) ✓", len(out), r.EventCount())
}

func TestWriteHTML(t *testing.T) {
	r := NewRecorder()
	r.Message(NodeGNB, NodeAMF, "NGSetupRequest", "TS 38.413 §9.2.6.1")
	r.Message(NodeAMF, NodeGNB, "NGSetupResponse", "TS 38.413 §9.2.6.2")

	path := t.TempDir() + "/test.html"
	if err := r.WriteHTML(path); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	// Read back and verify it's valid HTML with Mermaid
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	html := string(data)

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(html, "mermaid") {
		t.Error("missing mermaid reference")
	}
	if !strings.Contains(html, "NGSetupRequest") {
		t.Error("missing NGSetupRequest in HTML")
	}

	t.Logf("HTML file: %d bytes ✓", len(html))
}

func TestWriteMmd(t *testing.T) {
	r := NewRecorder()
	r.Message(NodeGNB, NodeAMF, "NGSetupRequest", "TS 38.413 §9.2.6.1")

	path := t.TempDir() + "/test.mmd"
	if err := r.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Log("WriteFile .mmd ✓")
}
