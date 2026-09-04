package channel

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func configureCooldownTest(t *testing.T, cfg *ChannelHealthSetting, now *time.Time) {
	t.Helper()

	previous := *GetChannelHealthSetting()
	previousNow := ChannelHealthNow
	SetChannelHealthSetting(cfg)
	if now != nil {
		ChannelHealthNow = func() time.Time { return *now }
	}
	t.Cleanup(func() {
		ChannelHealthNow = previousNow
		SetChannelHealthSetting(&previous)
	})
}

func cooldownTestSetting() *ChannelHealthSetting {
	return DefaultChannelHealthSetting()
}

func TestChannelCooldownTriggersOnConsecutiveThrottleAndFatal(t *testing.T) {
	mgr := resetChannelHealthManagerForTest()
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.MinRequests = 0
	cfg.CooldownThreshold = 2
	cfg.CooldownMaxEjectionPercent = 100
	configureCooldownTest(t, cfg, &now)

	const channelID = 9901
	throttle := ClassifyChannelOutcome(upstreamError("rate_limit_exceeded", 429), channelID)
	require.Equal(t, OutcomeThrottled, throttle)
	mgr.RecordChannelOutcome(channelID, throttle)
	assert.Greater(t, mgr.EffectiveWeight(channelID, 10), 0.0)

	fatal := ClassifyChannelOutcome(upstreamError("upstream_error", 503), channelID)
	require.Equal(t, OutcomeFatal, fatal)
	mgr.RecordChannelOutcome(channelID, fatal)
	assert.Zero(t, mgr.EffectiveWeight(channelID, 10), "the threshold outcome must eject the channel")

	mgr.Reset()
	mgr.RecordChannelOutcome(channelID, OutcomeThrottled)
	mgr.RecordChannelOutcome(channelID, OutcomeNeutral)
	mgr.RecordChannelOutcome(channelID, OutcomeFatal)
	assert.Greater(t, mgr.EffectiveWeight(channelID, 10), 0.0, "neutral breaks the consecutive failure run")
}

func TestChannelCooldownDurationSlidesFromBaseTowardMaximum(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownBaseSeconds = 30
	cfg.CooldownMaxSeconds = 60
	cfg.CooldownAlpha = 0.3

	assert.Equal(t, 30*time.Second, CooldownDuration(cfg, 0))
	assert.InDelta(t, float64(51*time.Second), float64(CooldownDuration(cfg, 1)), 1)
	assert.InDelta(t, 57.3*float64(time.Second), float64(CooldownDuration(cfg, 2)), 1)
	assert.LessOrEqual(t, CooldownDuration(cfg, 100), 60*time.Second)
}

func TestChannelCooldownExpiryReentersSlowStartAndDecaysStreak(t *testing.T) {
	mgr := resetChannelHealthManagerForTest()
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.MinRequests = 5
	cfg.CooldownThreshold = 1
	cfg.CooldownMaxEjectionPercent = 100
	configureCooldownTest(t, cfg, &now)

	const channelID = 9902
	mgr.RecordChannelOutcome(channelID, OutcomeFatal)
	require.Zero(t, mgr.EffectiveWeight(channelID, 10))

	now = now.Add(31 * time.Second)
	assert.InDelta(t, 2.0, mgr.EffectiveWeight(channelID, 10), 1e-9)

	snap, ok := mgr.SnapshotCooldownStateForTest(channelID)
	require.True(t, ok)
	assert.Zero(t, snap.RequestCount)
	assert.True(t, snap.RampPending)
	assert.False(t, snap.RampExited)
	assert.Equal(t, 1, snap.CooldownStreak)

	mgr.RecordChannelOutcome(channelID, OutcomeSuccess)
	// That success clears rampPending and sets requestCount=1. requestCount(1)
	// <= MinRequests(5) so the score stays 1.0, and the ramp factor is 1/5.
	assert.InDelta(t, 2.0, mgr.EffectiveWeight(channelID, 10), 1e-9)

	snap2, ok := mgr.SnapshotCooldownStateForTest(channelID)
	require.True(t, ok)
	assert.Zero(t, snap2.CooldownStreak, "a clean recovered outcome decays the cooldown streak")
}

func TestChannelCooldownKillSwitchAndLegacyRecordOutcome(t *testing.T) {
	mgr := resetChannelHealthManagerForTest()
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.MinRequests = 0
	cfg.CooldownThreshold = 1
	configureCooldownTest(t, cfg, &now)

	const channelID = 9903
	mgr.RecordOutcome(channelID, false)
	assert.Greater(t, mgr.EffectiveWeight(channelID, 10), 0.0, "legacy bool API must not start cooldown")

	mgr.RecordChannelOutcome(channelID, OutcomeFatal)
	require.Zero(t, mgr.EffectiveWeight(channelID, 10))

	cfg.Enabled = false
	SetChannelHealthSetting(cfg)
	assert.InDelta(t, 10.0, mgr.EffectiveWeight(channelID, 10), 1e-9)
}

func TestFilterCoolingChannelsHonorsEjectionCap(t *testing.T) {
	mgr := resetChannelHealthManagerForTest()
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.CooldownThreshold = 1
	cfg.CooldownMaxEjectionPercent = 50
	configureCooldownTest(t, cfg, &now)

	mgr.RecordChannelOutcome(9904, OutcomeFatal)
	mgr.RecordChannelOutcome(9905, OutcomeFatal)

	// Both candidates are cooling, so the tier is fully cooling: the cap does not
	// apply and the whole tier is ejected so selection fails fast.
	assert.Equal(t, map[int]bool{9904: true, 9905: true},
		mgr.FilterCoolingChannels([]int{9905, 9904}, 50))
	assert.Equal(t, map[int]bool{9904: true, 9905: true},
		mgr.FilterCoolingChannels([]int{9904, 9905}, 100))
	assert.Empty(t, mgr.FilterCoolingChannels([]int{9904, 9905}, 0),
		"zero percent disables cooldown ejection")

	// A partially cooling tier honours the cap: 1 of 2 candidates cooling at 50%
	// ejects exactly that one, and the healthy peer is untouched.
	mgr.Reset()
	mgr.RecordChannelOutcome(9904, OutcomeFatal)
	assert.Equal(t, map[int]bool{9904: true},
		mgr.FilterCoolingChannels([]int{9905, 9904}, 50))
}

// TestChannelCooldownSelectionSkipsEjectedTierInMemoryAndDB covers both halves of
// the cooldown ejection contract, which now run on two different mechanisms.
//
// The memory-cache path went flat and route-keyed with the route-unit cutover:
// there are no tiers to skip, and ejection is expressed per route unit by the
// health state machine rather than by the channel-level cooldown manager. The
// priority-tier DB path this test also used to cover is retired: nothing selects
// by descending priority tier any more, so there is no second path to diverge
// from.
func TestChannelCooldownSelectionSkipsEjectedRouteUnits(t *testing.T) {
	const group, modelName = "cooldown-group", "cooldown-model"
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.MinRequests = 0
	cfg.CooldownThreshold = 1
	cfg.CooldownMaxEjectionPercent = 100
	configureCooldownTest(t, cfg, &now)

	cooled := testChannel(9906)
	fallback := testChannel(9907)
	withChannelCacheFixture(t, []*Channel{cooled, fallback}, group, modelName,
		map[int]int{cooled.Id: 10, fallback.Id: 10})

	withRouteHealthDB(t)
	withHealthSetting(t, DefaultChannelModelHealthSetting())
	cooledKey := RouteKey{ChannelId: cooled.Id, KeyIndex: 0, Model: modelName}
	require.NoError(t, DisableRoute(cooledKey, time.Now()))

	// The ejected route unit is gone from the pool at every retry value, because
	// retry no longer selects a tier: the surviving route serves all of it.
	for _, retry := range []int{0, 1, 7} {
		counts := map[int]int{}
		for range 50 {
			got, err := GetRandomSatisfiedChannel(group, modelName, retry, "", nil)
			require.NoError(t, err)
			require.NotNil(t, got, "the healthy route must serve regardless of retry")
			counts[got.ChannelId]++
		}
		assert.Zero(t, counts[cooled.Id], "a disabled route unit must never be selected")
		assert.Equal(t, 50, counts[fallback.Id])
	}

	// Ejecting the survivor as well empties the pool, which is how selection fails
	// fast instead of handing back an ejected route.
	require.NoError(t, DisableRoute(RouteKey{ChannelId: fallback.Id, KeyIndex: 0, Model: modelName}, time.Now()))
	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
	require.NoError(t, err)
	assert.Nil(t, got, "with every route unit ejected selection must yield nothing")
}

func TestChannelCooldownDurationSlidesFromBaseTenTowardMaximum(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownBaseSeconds = 10
	cfg.CooldownMaxSeconds = 60
	cfg.CooldownAlpha = 0.3

	// n=0: exactly base
	assert.Equal(t, 10*time.Second, CooldownDuration(cfg, 0))
	// n=1: 10 + 50*(1-0.3) = 45.0
	assert.InDelta(t, 45.0*float64(time.Second), float64(CooldownDuration(cfg, 1)), 1)
	// n=2: 10 + 50*(1-0.3^2) = 10 + 50*(1-0.09) = 55.5
	assert.InDelta(t, 55.5*float64(time.Second), float64(CooldownDuration(cfg, 2)), 1)
	// large n approaches but never exceeds max
	assert.LessOrEqual(t, CooldownDuration(cfg, 100), 60*time.Second)
}

// resetChannelHealthManagerForTest creates a fresh health-manager singleton
// through the test seam and restores nothing: each call re-resets.
func resetChannelHealthManagerForTest() *ChannelHealthManager {
	return ResetChannelHealthManagerForTest()
}

// upstreamError builds the kind of error the relay layer produces for an
// upstream HTTP failure, mirroring model's test helper.
func upstreamError(code types.ErrorCode, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New("simulated upstream failure"), code, status)
}
