package model

import (
	rootmodel "github.com/QuantumNous/new-api/model"
)

func init() {
	rootmodel.RegisterEntities(
		&Channel{},
		&Ability{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&ProxyNode{},
		&GatewayConfigRevision{},
		&GatewayConfigOutbox{},
	)

	rootmodel.RegisterPostMigrateHook(InitializeGatewayConfigRevision)
}
