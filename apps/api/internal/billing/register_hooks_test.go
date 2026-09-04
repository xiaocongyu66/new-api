package billing_test

import (
	"testing"

	_ "github.com/QuantumNous/new-api/internal/billing"
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/stretchr/testify/assert"
)

// Importing the billing domain must install every hook the packages below it
// depend on. Each call site is nil-guarded, so losing a registration breaks
// nothing at compile time and only shows up at runtime as silently skipped
// work: an option group that stops loading, a payment gate that denies
// everything, or quota strings that render in the wrong currency.
//
// identity, logger, catalog, and settings all sit below billing and cannot
// import it, so these hooks are the only path for that behavior.
func TestPackageInitInstallsCrossDomainHooks(t *testing.T) {
	assert.NotNil(t, settings.OnSeedPaymentOptions,
		"payment option defaults must reach the option map")
	assert.NotNil(t, settings.OnApplyPaymentOption,
		"a persisted payment option must reach its typed target")
	assert.NotNil(t, settings.OnIsToolPriceOptionKey,
		"the tool-price option key must be recognized")
	assert.NotNil(t, settings.OnValidateToolPriceOption,
		"an invalid tool-price payload must be rejected")
	assert.NotNil(t, settings.OnApplyToolPriceOption,
		"a tool-price update must reach the price table")
	assert.NotNil(t, identity.OnIsPaymentComplianceConfirmed,
		"the payment compliance gate denies every top-up while unregistered")
	assert.NotNil(t, logger.OnFormatQuota,
		"quota strings render as USD regardless of the configured display type while unregistered")
	assert.NotNil(t, catalog.OnResolveTieredBilling,
		"tiered pricing disappears from the pricing snapshot while unregistered")
}
