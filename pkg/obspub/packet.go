package obspub

import (
	"sync"
	"time"

	"github.com/afroash/5g-sim/pkg/seqdiag"
)

const packetEmitMinInterval = 200 * time.Millisecond

var packetThrottleMu sync.Mutex
var packetThrottleLast = make(map[string]time.Time)

// EmitPacket publishes a user-plane packet observation (rate-limited per edge).
func EmitPacket(from, to seqdiag.Node, direction, summary, specRef string, fields map[string]string) {
	if !Enabled() {
		return
	}
	key := string(from) + "|" + string(to) + "|" + direction
	now := time.Now()
	packetThrottleMu.Lock()
	if last, ok := packetThrottleLast[key]; ok && now.Sub(last) < packetEmitMinInterval {
		packetThrottleMu.Unlock()
		return
	}
	packetThrottleLast[key] = now
	packetThrottleMu.Unlock()

	ev := Event{
		ID:        nextID(),
		TS:        now,
		Kind:      "packet",
		From:      string(from),
		To:        string(to),
		Type:      summary,
		Detail:    summary,
		Spec:      specRef,
		Component: string(from),
		Level:     "INFO",
		Fields:    fields,
	}
	if direction != "" && ev.Fields == nil {
		ev.Fields = map[string]string{"direction": direction}
	} else if direction != "" {
		ev.Fields["direction"] = direction
	}
	Emit(ev)
}
