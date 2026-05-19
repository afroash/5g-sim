package obspub

import "github.com/afroash/5g-sim/pkg/seqdiag"

// ProcedureWithDetail emits a procedure step to the observatory (synthetic RRC/N2/NAS labels).
// typ is the short message name in the GUI; detail is the longer description.
func ProcedureWithDetail(from, to seqdiag.Node, typ, detail, specRef string, fields map[string]string) {
	if !Enabled() {
		return
	}
	ev := FromProcedure(from, to, typ, specRef, fields)
	if detail != "" {
		ev.Detail = detail
	}
	Emit(ev)
}
