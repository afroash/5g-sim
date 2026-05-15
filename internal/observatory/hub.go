package observatory

import (
	"fmt"
	"sync"
	"time"

	"github.com/afroash/5g-sim/pkg/obspub"
)

// Hub buffers events and fans out to WebSocket subscribers.
type Hub struct {
	mu          sync.RWMutex
	events      []obspub.Event
	max         int
	subscribers map[chan obspub.Event]struct{}
}

// NewHub creates an event hub with the given buffer capacity.
func NewHub(max int) *Hub {
	if max <= 0 {
		max = 500
	}
	return &Hub{
		max:         max,
		subscribers: make(map[chan obspub.Event]struct{}),
	}
}

// Add stores and broadcasts an event.
func (h *Hub) Add(ev obspub.Event) {
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("ev-%d", time.Now().UnixNano())
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now()
	}
	h.mu.Lock()
	h.events = append(h.events, ev)
	if len(h.events) > h.max {
		h.events = h.events[len(h.events)-h.max:]
	}
	subs := make([]chan obspub.Event, 0, len(h.subscribers))
	for ch := range h.subscribers {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Recent returns the last n events (oldest first within the window).
func (h *Hub) Recent(n int) []obspub.Event {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if n <= 0 || len(h.events) == 0 {
		return nil
	}
	if n > len(h.events) {
		n = len(h.events)
	}
	out := make([]obspub.Event, n)
	copy(out, h.events[len(h.events)-n:])
	return out
}

// Subscribe registers a client channel. Caller must call Unsubscribe when done.
func (h *Hub) Subscribe(ch chan obspub.Event) {
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
}

// Unsubscribe removes a client channel.
func (h *Hub) Unsubscribe(ch chan obspub.Event) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	h.mu.Unlock()
}

// Clear removes buffered events.
func (h *Hub) Clear() {
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
}
