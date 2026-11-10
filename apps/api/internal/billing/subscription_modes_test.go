package billing

import (
	"testing"
	"time"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A request-count plan whose expiry axis is time grants its counts for a real
// term: the purchased row carries end_time and still expands its counters.
func TestPurchaseRequestPlanWithTimeExpiryCarriesTerm(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9311, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o", "claude-sonnet"}, Count: 3}},
		0)
	plan.ExpireMode = SubscriptionExpireModeTime
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Update("expire_mode", SubscriptionExpireModeTime).Error)

	var sub *UserSubscription
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		purchased, err := CreateUserSubscriptionFromPlanTx(tx, 9311, plan, "test")
		sub = purchased
		return err
	}))
	require.NotNil(t, sub)
	assert.Equal(t, SubscriptionKindRequest, sub.Kind)
	assert.Greater(t, sub.EndTime, sub.StartTime, "time-expiry request plan must carry end_time")
	assert.Equal(t, int64(0), sub.NextResetTime, "request plans never reset their counts")

	var counters []SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).Find(&counters).Error)
	assert.Len(t, counters, 2)
}

// The expiry axes only end the subscription earlier of the two: a request
// subscription whose term passed expires by time even with counts left.
func TestExpireDueSubscriptionsExpiresRequestKindByTime(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9312, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 3}},
		0)
	sub := activeSubscriptionForKindTest(t, 9312, plan)
	// activeSubscriptionForKindTest only endows time-kind rows with a term, so
	// emulate a purchase made under the time-expiry axis.
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).
		Update("end_time", common.GetTimestamp()-60).Error)

	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)

	var reloaded UserSubscription
	require.NoError(t, dbx.DB.First(&reloaded, sub.Id).Error)
	assert.Equal(t, "expired", reloaded.Status)
}

// A request subscription without a term ends only when every counter is spent.
func TestExpireDueSubscriptionsExpiresRequestKindByExhaustion(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9313, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 2}},
		0)
	fresh := activeSubscriptionForKindTest(t, 9313, plan)

	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 0, expired)

	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: fresh.Id,
		GroupName:          "vip",
		ModelName:          "gpt-4o",
		Total:              2,
		Used:               2,
	}).Error)

	expired, err = ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)

	var reloaded UserSubscription
	require.NoError(t, dbx.DB.First(&reloaded, fresh.Id).Error)
	assert.Equal(t, "expired", reloaded.Status)
}

// seedModeTestRatios installs the group ratios the rule validator consults.
// The billing test binary never runs InitRatioSettings, so the maps start
// empty and every configured group would otherwise be rejected.
func seedModeTestRatios(t *testing.T) {
	t.Helper()
	savedGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatios)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1.5,"svip":2}`))
}

// The (group, model) pair is the counter key, so the same pair under two rules
// would silently grant the sum of both counts under a single bar.
func TestApplyPlanRequestRulesRejectsDuplicateGroupModel(t *testing.T) {
	seedModeTestRatios(t)
	plan := &SubscriptionPlan{
		Kind:             SubscriptionKindRequest,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		QuotaResetPeriod: SubscriptionResetNever,
		RequestRules: []SubscriptionRequestRule{
			{Group: "vip", Models: []string{"gpt-4o", "claude-sonnet"}, Count: 1},
			{Group: "vip", Models: []string{"claude-sonnet"}, Count: 5},
		},
	}
	err := applyPlanRequestRules(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude-sonnet")
}

// The same model may appear under several groups: those are distinct counters.
func TestApplyPlanRequestRulesAcceptsModelAcrossGroups(t *testing.T) {
	seedModeTestRatios(t)
	plan := &SubscriptionPlan{
		Kind:             SubscriptionKindRequest,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		QuotaResetPeriod: SubscriptionResetNever,
		RequestRules: []SubscriptionRequestRule{
			{Group: "vip", Models: []string{"gpt-4o"}, Count: 1},
			{Group: "svip", Models: []string{"gpt-4o"}, Count: 5},
		},
	}
	require.NoError(t, applyPlanRequestRules(plan))
	require.Len(t, plan.RequestRules, 2)
}

// Legacy rows (no expire_mode) keep their historical meaning: quota and
// request kinds end by exhaustion, time kinds end at end_time.
func TestCalcPlanEndTimeFallsBackToKindForLegacyRows(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		kind string
		want int64
	}{
		{SubscriptionKindTime, now.AddDate(0, 1, 0).Unix()},
		{SubscriptionKindQuota, 0},
		{SubscriptionKindRequest, 0},
	}
	for _, tc := range cases {
		plan := &SubscriptionPlan{
			Kind:          tc.kind,
			DurationUnit:  SubscriptionDurationMonth,
			DurationValue: 1,
		}
		got, err := calcPlanEndTime(now, plan)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "kind %s", tc.kind)
	}

	timeRequest := &SubscriptionPlan{
		Kind:          SubscriptionKindRequest,
		ExpireMode:    SubscriptionExpireModeTime,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
	}
	got, err := calcPlanEndTime(now, timeRequest)
	require.NoError(t, err)
	assert.Equal(t, now.AddDate(0, 1, 0).Unix(), got)
}

// The two admin-form axes collapse into the single stored kind.
func TestDeriveSubscriptionKind(t *testing.T) {
	assert.Equal(t, SubscriptionKindTime, deriveSubscriptionKind(SubscriptionLimitModeGroup, SubscriptionExpireModeTime))
	assert.Equal(t, SubscriptionKindQuota, deriveSubscriptionKind(SubscriptionLimitModeGroup, SubscriptionExpireModeQuota))
	assert.Equal(t, SubscriptionKindRequest, deriveSubscriptionKind(SubscriptionLimitModeRequest, SubscriptionExpireModeTime))
	assert.Equal(t, SubscriptionKindRequest, deriveSubscriptionKind(SubscriptionLimitModeRequest, SubscriptionExpireModeQuota))
}

// normalizeExpireMode keeps stored values and falls back per kind.
func TestNormalizeExpireModeFallbacks(t *testing.T) {
	assert.Equal(t, SubscriptionExpireModeQuota, normalizeExpireMode("", SubscriptionKindRequest))
	assert.Equal(t, SubscriptionExpireModeQuota, normalizeExpireMode("", SubscriptionKindQuota))
	assert.Equal(t, SubscriptionExpireModeTime, normalizeExpireMode("", SubscriptionKindTime))
	assert.Equal(t, SubscriptionExpireModeTime, normalizeExpireMode(SubscriptionExpireModeTime, SubscriptionKindRequest))
	assert.Equal(t, SubscriptionExpireModeTime, normalizeExpireMode("garbage", SubscriptionKindTime))
}

// Wallet fallback is a term-scoped add-on: a quota-expiry plan ends exactly
// when its pool is spent, so it must not carry an overflow promise.
func TestQuotaExpiryPlansForciblyDropWalletOverflow(t *testing.T) {
	plan := &SubscriptionPlan{
		Kind:                SubscriptionKindQuota,
		ExpireMode:          SubscriptionExpireModeQuota,
		TotalAmount:         10,
		AllowWalletOverflow: common.GetPointer(true),
	}
	require.NoError(t, applySubscriptionKindConstraints(plan))
	require.NotNil(t, plan.AllowWalletOverflow)
	assert.False(t, *plan.AllowWalletOverflow)

	timePlan := &SubscriptionPlan{
		Kind:                SubscriptionKindTime,
		ExpireMode:          SubscriptionExpireModeTime,
		AllowWalletOverflow: common.GetPointer(true),
	}
	require.NoError(t, applySubscriptionKindConstraints(timePlan))
	require.NotNil(t, timePlan.AllowWalletOverflow)
	assert.True(t, *timePlan.AllowWalletOverflow)
}
