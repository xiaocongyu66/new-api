package channel

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/QuantumNous/new-api/model"
)

// The group-models lookup, the gateway routing revision, and the pricing cache
// live in this domain, and identity / model / settings cannot reach them on
// their own, so all three entry points are registered here. Registering from
// this init() rather than from main.InitResources() is deliberate: the call
// sites are nil-guarded, so a bootstrap-only registration silently skips the
// work in every test binary that imports this package without going through
// main. TestPackageInitInstallsCrossDomainHooks pins the first two.
//
// settings owns the billing_setting option but not the pricing cache that a
// tiered-config change invalidates, so that entry point arrives the same way.
func init() {
	identity.RegisterGroupModelsResolver(GetGroupsEnabledModels)
	model.MutateGatewayRoutingFn = MutateGatewayRouting
	settings.OnBillingSettingChanged = InvalidatePricingCache
}
