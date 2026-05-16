package observatory

import (
	"strings"
	"testing"

	"github.com/afroash/5g-sim/internal/ue"
)

func TestParseSpawnUEBody_empty(t *testing.T) {
	opts, err := ParseSpawnUEBody(nil)
	if err != nil || opts.Profile != ue.ProfileLocal || opts.SUPI != "" {
		t.Fatalf("got %+v err=%v", opts, err)
	}
	opts, err = ParseSpawnUEBody([]byte("   \n"))
	if err != nil || opts.Profile != ue.ProfileLocal {
		t.Fatalf("got %+v", opts)
	}
}

func TestParseSpawnUEBody_JSON(t *testing.T) {
	opts, err := ParseSpawnUEBody([]byte(`{"profile":"clab","supi":"imsi-001010000000007"}`))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Profile != "clab" || opts.SUPI != "imsi-001010000000007" {
		t.Fatalf("got %+v", opts)
	}
}

func TestParseSpawnUEBody_badProfile(t *testing.T) {
	_, err := ParseSpawnUEBody([]byte(`{"profile":"fabric"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want unknown profile error, got %v", err)
	}
}
