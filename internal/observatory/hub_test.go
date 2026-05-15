package observatory

import (
	"testing"

	"github.com/afroash/5g-sim/pkg/obspub"
)

func TestHubBufferAndBroadcast(t *testing.T) {
	h := NewHub(3)
	ch := make(chan obspub.Event, 4)
	h.Subscribe(ch)

	for i := 0; i < 5; i++ {
		h.Add(obspub.Event{ID: string(rune('a' + i)), Kind: "test"})
	}

	recent := h.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("buffer cap: got %d want 3", len(recent))
	}

	got := 0
	for {
		select {
		case <-ch:
			got++
		default:
			if got < 3 {
				t.Fatalf("expected at least 3 broadcasts, got %d", got)
			}
			return
		}
	}
}
