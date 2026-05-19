package smf

import (
	"fmt"

	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/afroash/5g-sim/pkg/seqdiag"
)

func emitPFCPProcedure(supi string, ulTEID uint32, ueIP string) {
	obspub.ProcedureWithDetail(seqdiag.NodeSMF, seqdiag.NodeUPF,
		"PFCP Session Est.", "FAR/PDR rules pushed", "TS 29.244 §6.3.3",
		map[string]string{
			"supi":    supi,
			"ul_teid": fmt.Sprintf("0x%08X", ulTEID),
			"ue_ip":   ueIP,
		})
}

func emitCreateSMContext(supi, dnn string) {
	obspub.ProcedureWithDetail(seqdiag.NodeAMF, seqdiag.NodeSMF,
		"Nsmf_PDUSession_Create", "DNN: "+dnn, "TS 29.502 §5.2.2.2",
		map[string]string{"supi": supi, "dnn": dnn})
}
