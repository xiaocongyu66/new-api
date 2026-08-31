package channel

// The 12 RouteStats* option keys are only useful if a persisted value actually
// reaches the runtime setting the scheduler reads. configure_route_stats.go had no
// init() at all, so an operator could save a value, see it stored, and have the
// scheduler keep running on defaults forever.
//
// These tests drive settings.ApplyOption / settings.ValidateOptionValue — the
// production dispatch — rather than calling UpdateRouteStatsSettingValue
// directly, so deleting the hook registration fails them.

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoreRouteStatsSetting puts the global setting back, so a test that changes it
// cannot leak into the rest of the package. It also ensures common.OptionMap
// exists: ApplyOption records every applied value there, and this package's tests
// do not run settings.SeedOptionMap.
func restoreRouteStatsSetting(t *testing.T) {
	t.Helper()
	previous := routestats.GetRouteStatsSetting()
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		routestats.SetRouteStatsSetting(previous)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
	})
}

func TestApplyOptionReachesRouteStatsRuntimeSetting(t *testing.T) {
	restoreRouteStatsSetting(t)

	require.True(t, settings.OnIsRouteStatsOptionKey != nil,
		"catalog must register the route stats option-key hook from its init")

	// A value the scheduler reads on every selection: the share-correction window.
	require.NoError(t, settings.ApplyOption("RouteStatsShareWindowSize", "37"))
	assert.Equal(t, 37, routestats.GetRouteStatsSetting().ShareWindowSize,
		"a persisted RouteStats option must reach the runtime setting, not just OptionMap")

	require.NoError(t, settings.ApplyOption("RouteStatsMinSamples", "9"))
	assert.Equal(t, 9, routestats.GetRouteStatsSetting().MinSamples)

	require.NoError(t, settings.ApplyOption("RouteStatsEnabled", "false"))
	assert.False(t, routestats.GetRouteStatsSetting().Enabled)
}

// An invalid value must be rejected by the dispatch rather than stored, otherwise
// the admin panel would report success for a setting the scheduler ignored.
func TestApplyOptionRejectsInvalidRouteStatsValue(t *testing.T) {
	restoreRouteStatsSetting(t)

	before := routestats.GetRouteStatsSetting().ShareWindowSize

	require.Error(t, settings.ApplyOption("RouteStatsShareWindowSize", "not-a-number"))
	assert.Equal(t, before, routestats.GetRouteStatsSetting().ShareWindowSize,
		"a rejected value must leave the runtime setting untouched")

	assert.Error(t, settings.ValidateOptionValue("RouteStatsShareWindowSize", "not-a-number"),
		"validation must run through the registered hook")
	assert.NoError(t, settings.ValidateOptionValue("RouteStatsShareWindowSize", "50"))
}

// Every advertised key must be seeded, so the admin panel shows the value the
// scheduler is actually using instead of an empty field.
func TestRouteStatsOptionsAreSeeded(t *testing.T) {
	seeded := seedRouteStatsOptions()

	for key := range RouteStatsSettingOptionKeys {
		value, ok := seeded[key]
		assert.True(t, ok, "option %s is advertised but never seeded", key)
		assert.NotEmpty(t, value, "option %s seeded with an empty value", key)
	}
	assert.Len(t, seeded, len(RouteStatsSettingOptionKeys),
		"the seed must cover exactly the advertised keys")
}

// The seed hook has to be chained, not assigned: several catalog files install
// their own and an assignment would silently drop the others.
func TestRouteStatsSeedIsChainedWithOtherCatalogOptions(t *testing.T) {
	require.NotNil(t, settings.OnSeedCatalogOptions)

	all := settings.OnSeedCatalogOptions()
	assert.Contains(t, all, "RouteStatsShareWindowSize",
		"route stats keys must survive the chained seed")
	assert.Contains(t, all, OptChannelHealthEnabled,
		"chaining must not drop a sibling domain's seeded options")
}
