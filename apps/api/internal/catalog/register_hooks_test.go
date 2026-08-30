package channel_test

import (
	"testing"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Importing this package must install every hook the domains below depend on.
// Each of these registrations has already been lost once by a package move: the
// call sites all guard against nil, so losing one breaks nothing at compile time
// and only shows up as silently skipped work at runtime.
func TestPackageInitInstallsCrossDomainHooks(t *testing.T) {
	assert.NotNil(t, settings.OnBillingSettingChanged,
		"a billing_setting change must invalidate the pricing cache")
	assert.NotNil(t, model.MutateGatewayRoutingFn,
		"a gateway-routing option write must bump the config revision")
}

// A billing_setting update must reach the pricing cache. Without the hook the
// cache keeps serving the superseded tiered expressions until the process
// restarts.
func TestBillingSettingUpdateInvalidatesPricingCache(t *testing.T) {
	truncateTables(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	// The shared TestMain migration omits these three; UpdateOption writes options
	// and the pricing rebuild reads models/vendors.
	require.NoError(t, dbx.DB.AutoMigrate(&model.Option{}, &catalog.Model{}, &catalog.Vendor{}))
	t.Cleanup(func() {
		require.NoError(t, dbx.DB.Exec("DELETE FROM options").Error)
	})
	// ApplyOption writes into common.OptionMap, which only SeedOptionMap creates.
	settings.SeedOptionMap()
	require.NoError(t, dbx.DB.Create(&catalog.Channel{
		Id:     7401,
		Type:   1,
		Key:    "key-7401",
		Status: common.ChannelStatusEnabled,
		Name:   "pricing-invalidation-channel",
	}).Error)
	require.NoError(t, dbx.DB.Create(&catalog.Ability{
		Group:     "default",
		Model:     "pricing-invalidation-model",
		ChannelId: 7401,
		Enabled:   true,
	}).Error)
	catalog.InitChannelCache()

	require.NotEmpty(t, catalog.GetPricing(), "the cache must be warm before the update")

	// Drop the row the warm snapshot was built from. Only a cache invalidation
	// makes the next read observe the deletion.
	require.NoError(t, dbx.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, model.UpdateOption("billing_setting.tiered.enabled", "false"))

	for _, pricing := range catalog.GetPricing() {
		assert.NotEqual(t, "pricing-invalidation-model", pricing.ModelName,
			"the pricing cache must be rebuilt after a billing_setting change")
	}
}
