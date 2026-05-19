package gnb

import (
	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

func (g *GNB) obsProcedure(from, to seqdiag.Node, typ, detail, spec string, fields map[string]string) {
	if g.Hub != nil {
		pairs := mapToKVPairs(fields)
		g.Hub.ProcedureWithDetail(from, to, typ, detail, spec, pairs...)
		return
	}
	obspub.ProcedureWithDetail(from, to, typ, detail, spec, fields)
}

func mapToKVPairs(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m)*2)
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}
