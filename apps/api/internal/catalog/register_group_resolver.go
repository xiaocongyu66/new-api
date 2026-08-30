package channel

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/QuantumNous/new-api/model"
)

// The catalog lookup reads channel records, so identity cannot import this
// package without reversing the dependency. It exposes a hook instead.

// The gateway routing revision and pricing cache live in this domain; model
// cannot import it (this package imports model), so both entry points are
// registered as hooks during startup. settings owns the billing_setting option
// but not the pricing cache that a tiered-config change invalidates, so that
// entry point arrives the same way.
func init() {
	identity.RegisterGroupModelsResolver(GetGroupsEnabledModels)
	model.MutateGatewayRoutingFn = MutateGatewayRouting
	settings.OnBillingSettingChanged = InvalidatePricingCache
}
