package amf

import (
	"github.com/afroash/5g-sim/internal/udm"
)

// udmRegister calls UDM Nudm_UECM to verify subscriber and register AMF 3GPP access.
func (a *AMF) udmRegister(supi string) (*udm.SubscriptionData, error) {
	client := udm.NewClient(a.config.UDMAddress)
	return client.RegisterAMF3GPPAccess(supi, a.config.InstanceID)
}
