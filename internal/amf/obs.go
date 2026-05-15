// obs.go — localhost observability API for the 5G Observatory (not 3GPP SBI).
package amf

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// obsUE is the JSON representation of a UE for GET /obs/v1/ues.
type obsUE struct {
	SUPI         string `json:"supi"`
	State        string `json:"state"`
	AllocatedIP  string `json:"allocatedIp,omitempty"`
	SMContextRef string `json:"smContextRef,omitempty"`
	RegisteredAt string `json:"registeredAt,omitempty"`
}

func (a *AMF) handleObsUEs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !obsLocalOnly(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	list := a.listObsUEs()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ues": list})
}

func obsLocalOnly(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func (a *AMF) listObsUEs() []obsUE {
	a.ues.mu.RLock()
	defer a.ues.mu.RUnlock()
	out := make([]obsUE, 0, len(a.ues.byNgapID))
	for _, ue := range a.ues.byNgapID {
		reg := ""
		if !ue.RegisteredAt.IsZero() {
			reg = ue.RegisteredAt.UTC().Format(time.RFC3339)
		}
		out = append(out, obsUE{
			SUPI:         ue.SUPI,
			State:        ue.State.String(),
			AllocatedIP:  ue.AllocatedIP,
			SMContextRef: ue.SMContextRef,
			RegisteredAt: reg,
		})
	}
	return out
}
