package channel

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate: a record
// moving into its own domain takes its registration with it (see the same
// contract in internal/common/dbx). Order matches the previous literal list.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &Ability{}, Name: "Ability"},
		dbx.Migration{Model: &Channel{}, Name: "Channel"},
		dbx.Migration{Model: &ChannelModelHealth{}, Name: "ChannelModelHealth"},
		dbx.Migration{Model: &ChannelModelRoute{}, Name: "ChannelModelRoute"},
		dbx.Migration{Model: &GatewayConfigOutbox{}, Name: "GatewayConfigOutbox"},
		dbx.Migration{Model: &GatewayConfigRevision{}, Name: "GatewayConfigRevision"},
		dbx.Migration{Model: &Model{}, Name: "Model"},
		dbx.Migration{Model: &PrefillGroup{}, Name: "PrefillGroup"},
		dbx.Migration{Model: &Vendor{}, Name: "Vendor"},
	)
}
