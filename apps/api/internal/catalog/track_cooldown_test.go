package channel

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/catalog/health_store"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func configureCooldownTest(t *testing.T, cfg *health_store.ChannelHealthSetting, now *time.Time) {
	t.Helper()

	previous := *health_store.GetChannelHealthSetting()
	previousNow := model.ChannelHealthNow
	health_store.SetChannelHealthSetting(cfg)
	if now != nil {
		model.ChannelHealthNow = func() time.Time { return *now }
	}
	t.Cleanup(func() {
		model.ChannelHealthNow = previousNow
		health_store.SetChannelHealthSetting(&previous)
	})
}

func cooldownTestSetting() *health_store.ChannelHealthSetting {
	return health_store.DefaultChannelHealthSetting()
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
	throttle := model.ClassifyChannelOutcome(upstreamError("rate_limit_exceeded", 429), channelID)
	require.Equal(t, model.OutcomeThrottled, throttle)
	mgr.RecordChannelOutcome(channelID, throttle)
	assert.Greater(t, mgr.EffectiveWeight(channelID, 10), 0.0)

	fatal := model.ClassifyChannelOutcome(upstreamError("upstream_error", 503), channelID)
	require.Equal(t, model.OutcomeFatal, fatal)
	mgr.RecordChannelOutcome(channelID, fatal)
	assert.Zero(t, mgr.EffectiveWeight(channelID, 10), "the threshold outcome must eject the channel")

	mgr.Reset()
	mgr.RecordChannelOutcome(channelID, model.OutcomeThrottled)
	mgr.RecordChannelOutcome(channelID, model.OutcomeNeutral)
	mgr.RecordChannelOutcome(channelID, model.OutcomeFatal)
	assert.Greater(t, mgr.EffectiveWeight(channelID, 10), 0.0, "neutral breaks the consecutive failure run")
}

func TestChannelCooldownDurationSlidesFromBaseTowardMaximum(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownBaseSeconds = 30
	cfg.CooldownMaxSeconds = 60
	cfg.CooldownAlpha = 0.3

	assert.Equal(t, 30*time.Second, model.CooldownDuration(cfg, 0))
	assert.InDelta(t, float64(51*time.Second), float64(model.CooldownDuration(cfg, 1)), 1)
	assert.InDelta(t, 57.3*float64(time.Second), float64(model.CooldownDuration(cfg, 2)), 1)
	assert.LessOrEqual(t, model.CooldownDuration(cfg, 100), 60*time.Second)
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
	mgr.RecordChannelOutcome(channelID, model.OutcomeFatal)
	require.Zero(t, mgr.EffectiveWeight(channelID, 10))

	now = now.Add(31 * time.Second)
	assert.InDelta(t, 2.0, mgr.EffectiveWeight(channelID, 10), 1e-9)

	snap, ok := mgr.SnapshotCooldownStateForTest(channelID)
	require.True(t, ok)
	assert.Zero(t, snap.RequestCount)
	assert.True(t, snap.RampPending)
	assert.False(t, snap.RampExited)
	assert.Equal(t, 1, snap.CooldownStreak)

	mgr.RecordChannelOutcome(channelID, model.OutcomeSuccess)
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

	mgr.RecordChannelOutcome(channelID, model.OutcomeFatal)
	require.Zero(t, mgr.EffectiveWeight(channelID, 10))

	cfg.Enabled = false
	health_store.SetChannelHealthSetting(cfg)
	assert.InDelta(t, 10.0, mgr.EffectiveWeight(channelID, 10), 1e-9)
}

func TestFilterCoolingChannelsHonorsEjectionCap(t *testing.T) {
	mgr := resetChannelHealthManagerForTest()
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.CooldownThreshold = 1
	cfg.CooldownMaxEjectionPercent = 50
	configureCooldownTest(t, cfg, &now)

	mgr.RecordChannelOutcome(9904, model.OutcomeFatal)
	mgr.RecordChannelOutcome(9905, model.OutcomeFatal)

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
	mgr.RecordChannelOutcome(9904, model.OutcomeFatal)
	assert.Equal(t, map[int]bool{9904: true},
		mgr.FilterCoolingChannels([]int{9905, 9904}, 50))
}

func TestChannelCooldownSelectionSkipsEjectedTierInMemoryAndDB(t *testing.T) {
	const group, modelName = "cooldown-group", "cooldown-model"
	now := time.Unix(1_700_000_000, 0)
	cfg := cooldownTestSetting()
	cfg.MinRequests = 0
	cfg.CooldownThreshold = 1
	cfg.CooldownMaxEjectionPercent = 100
	configureCooldownTest(t, cfg, &now)

	cooled := testChannel(9906, 10, 100)
	fallback := testChannel(9907, 10, 10)
	withChannelCacheFixture(t, []*model.Channel{cooled, fallback}, group, modelName)
	mgr := resetChannelHealthManagerForTest()
	mgr.RecordChannelOutcome(cooled.Id, model.OutcomeFatal)

	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
	require.NoError(t, err)
	assert.Nil(t, got, "all candidates in the selected tier are ejected")
	got, err = GetRandomSatisfiedChannel(group, modelName, 1, "", nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, fallback.Id, got.Id)

	withAbilityDB(t, group, modelName, []model.Ability{
		ability(cooled.Id, group, modelName, 10, 100),
		ability(fallback.Id, group, modelName, 10, 10),
	})
	got, err = model.GetChannel(group, modelName, 0, "", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
	got, err = model.GetChannel(group, modelName, 1, "", nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, fallback.Id, got.Id)
}

func TestChannelCooldownDurationSlidesFromBaseTenTowardMaximum(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownBaseSeconds = 10
	cfg.CooldownMaxSeconds = 60
	cfg.CooldownAlpha = 0.3

	// n=0: exactly base
	assert.Equal(t, 10*time.Second, model.CooldownDuration(cfg, 0))
	// n=1: 10 + 50*(1-0.3) = 45.0
	assert.InDelta(t, 45.0*float64(time.Second), float64(model.CooldownDuration(cfg, 1)), 1)
	// n=2: 10 + 50*(1-0.3^2) = 10 + 50*(1-0.09) = 55.5
	assert.InDelta(t, 55.5*float64(time.Second), float64(model.CooldownDuration(cfg, 2)), 1)
	// large n approaches but never exceeds max
	assert.LessOrEqual(t, model.CooldownDuration(cfg, 100), 60*time.Second)
}

// resetChannelHealthManagerForTest creates a fresh health-manager singleton
// through the model test seam and restores nothing: each call re-resets.

// upstreamError builds the kind of error the relay layer produces for an
// upstream HTTP failure, mirroring model's test helper.
func upstreamError(code types.ErrorCode, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New("simulated upstream failure"), code, status)
}

// withAbilityDB installs an isolated in-memory abilities/channels database,
// mirroring the model-package fixture used by the selection tests.

// ability mirrors the model-package fixture constructor.
func ability(channelID int, group, modelName string, weight uint, priority int64) model.Ability {
	p := priority
	return model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
		Priority:  &p,
		Weight:    weight,
	}
}

func withAbilityDB(t *testing.T, group, modelName string, rows []model.Ability) {
	t.Helper()

	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Channel{}))
	dbx.DB = db
	// initCol() runs inside InitDB but tests bypass InitDB with a bare gorm.Open,
	// so initialize the dialect-correct column names (commonGroupCol etc.) now.
	model.InitDialectColumns()
	t.Cleanup(func() { dbx.DB = previousDB })

	for i := range rows {
		require.NoError(t, dbx.DB.Create(&rows[i]).Error)
		weight := rows[i].Weight
		priority := rows[i].Priority
		require.NoError(t, dbx.DB.Create(&model.Channel{
			Id:       rows[i].ChannelId,
			Weight:   &weight,
			Priority: priority,
			Status:   1,
		}).Error)
	}
}

func resetChannelHealthManagerForTest() *model.ChannelHealthManager {
	return model.ResetChannelHealthManagerForTest()
}

// setTestConfigCapability mirrors model's setTestConfig helper.
func setTestConfigCapability(enabled bool, alpha, minScore float64, minRequests int) {
	health_store.SetChannelHealthSetting(&health_store.ChannelHealthSetting{
		Enabled:     enabled,
		Alpha:       alpha,
		MinScore:    minScore,
		MinRequests: minRequests,
	})
}
