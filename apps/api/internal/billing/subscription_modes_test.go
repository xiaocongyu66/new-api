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
	assert.Equal(t, SubscriptionKindTime, deriveSubscriptionKind(SubscriptionScopeAll, SubscriptionMeasureQuota, SubscriptionExpireModeTime))
	assert.Equal(t, SubscriptionKindQuota, deriveSubscriptionKind(SubscriptionScopeAll, SubscriptionMeasureQuota, SubscriptionExpireModeQuota))
	assert.Equal(t, SubscriptionKindRequest, deriveSubscriptionKind(SubscriptionScopeModel, SubscriptionMeasureCount, SubscriptionExpireModeTime))
	assert.Equal(t, SubscriptionKindRequest, deriveSubscriptionKind(SubscriptionScopeModel, SubscriptionMeasureCount, SubscriptionExpireModeQuota))
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

// stampSubscriptionAxes writes the scope/measure snapshot onto a seeded
// subscription; the shared helper predates the columns.
func stampSubscriptionAxes(t *testing.T, subId int, scope string, measure string) {
	t.Helper()
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Updates(map[string]interface{}{
			"scope":   scope,
			"measure": measure,
		}).Error)
}

// A group-scoped quota plan creates one entitlement row per group (empty model
// name) and gates requests by that row's remaining quota.
func TestGroupScopedQuotaGatesAndSettles(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9321, SubscriptionKindTime, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":       SubscriptionScopeGroup,
			"measure":     SubscriptionMeasureQuota,
			"expire_mode": SubscriptionExpireModeTime,
		}).Error)
	plan.Scope = SubscriptionScopeGroup
	plan.Measure = SubscriptionMeasureQuota
	plan.ExpireMode = SubscriptionExpireModeTime

	sub := activeSubscriptionForKindTest(t, 9321, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeGroup, SubscriptionMeasureQuota)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              100,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	// First request reserves 40 of the 100-unit group budget.
	res, err := PreConsumeUserSubscription("scope-quota-1", 9321, "vip", "gpt-4o", 0, 40)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(40), reloadSubscriptionCounter(t, counter.Id).Used)
	assert.False(t, res.RequestKind, "quota-measured rows are not count-billed")

	// A request costlier than what is left must not match this subscription.
	_, err = PreConsumeUserSubscription("scope-quota-2", 9321, "vip", "gpt-4o", 0, 80)
	require.Error(t, err)
	assert.Equal(t, int64(40), reloadSubscriptionCounter(t, counter.Id).Used)

	// Refund releases exactly the reserved amount.
	require.NoError(t, RefundSubscriptionPreConsume("scope-quota-1"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
}

// A group-scoped count plan counts one unit per request regardless of model.
func TestGroupScopedCountIgnoresModel(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9322, SubscriptionKindRequest, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeGroup,
			"measure": SubscriptionMeasureCount,
		}).Error)
	plan.Scope = SubscriptionScopeGroup
	plan.Measure = SubscriptionMeasureCount

	sub := activeSubscriptionForKindTest(t, 9322, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeGroup, SubscriptionMeasureCount)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureCount,
		Total:              2,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	for _, model := range []string{"gpt-4o", "claude"} {
		_, err := PreConsumeUserSubscription("scope-count-"+model, 9322, "vip", model, 0, 10)
		require.NoError(t, err)
	}
	assert.Equal(t, int64(2), reloadSubscriptionCounter(t, counter.Id).Used)

	// Exhausted: a third request of any model is rejected.
	_, err := PreConsumeUserSubscription("scope-count-3", 9322, "vip", "gpt-4o", 0, 10)
	require.Error(t, err)
}

// use a model-scoped quota plan: each (group, model) row is its own money pool.
func TestModelScopedQuotaRowsAreIndependent(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9323, SubscriptionKindTime, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeModel,
			"measure": SubscriptionMeasureQuota,
		}).Error)
	plan.Scope = SubscriptionScopeModel
	plan.Measure = SubscriptionMeasureQuota

	sub := activeSubscriptionForKindTest(t, 9323, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeModel, SubscriptionMeasureQuota)
	for model, total := range map[string]int64{"gpt-4o": 50, "claude": 10} {
		require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
			UserSubscriptionId: sub.Id,
			GroupName:          "vip",
			ModelName:          model,
			Measure:            SubscriptionMeasureQuota,
			Total:              total,
		}).Error)
	}

	_, err := PreConsumeUserSubscription("model-quota-1", 9323, "vip", "claude", 0, 30)
	require.Error(t, err, "claude only has 10 units")

	_, err = PreConsumeUserSubscription("model-quota-2", 9323, "vip", "gpt-4o", 0, 30)
	require.NoError(t, err)
}

// Quota-measured scopes are money pools: they must stay usable for non-text
// relay formats, which gate request counts off via allowRequestCount=false.
func TestQuotaScopedSubscriptionUsableForNonTextFormats(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9331, SubscriptionKindTime, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeGroup,
			"measure": SubscriptionMeasureQuota,
		}).Error)
	plan.Scope = SubscriptionScopeGroup
	plan.Measure = SubscriptionMeasureQuota
	sub := activeSubscriptionForKindTest(t, 9331, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeGroup, SubscriptionMeasureQuota)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              100,
	}).Error)

	usable, err := HasUsableUserSubscription(9331, "vip", "gpt-image-1", false)
	require.NoError(t, err)
	assert.True(t, usable, "quota-measured scopes must serve non-text formats")

	// Count-measured scopes stay text-only.
	countPlan := seedSubscriptionKindPlan(t, 9332, SubscriptionKindRequest, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", countPlan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeGroup,
			"measure": SubscriptionMeasureCount,
		}).Error)
	countPlan.Scope = SubscriptionScopeGroup
	countPlan.Measure = SubscriptionMeasureCount
	countSub := activeSubscriptionForKindTest(t, 9332, countPlan)
	stampSubscriptionAxes(t, countSub.Id, SubscriptionScopeGroup, SubscriptionMeasureCount)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: countSub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureCount,
		Total:              5,
	}).Error)
	usable, err = HasUsableUserSubscription(9332, "vip", "gpt-image-1", false)
	require.NoError(t, err)
	assert.False(t, usable, "count-measured scopes keep the text-only contract")
}

// A waived quota-scoped request must release its reservation exactly once: the
// settle delta after waive must not touch the row again.
func TestQuotaScopeWaiveThenSettleReleasesOnce(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9341, SubscriptionKindTime, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeGroup,
			"measure": SubscriptionMeasureQuota,
		}).Error)
	plan.Scope = SubscriptionScopeGroup
	plan.Measure = SubscriptionMeasureQuota
	sub := activeSubscriptionForKindTest(t, 9341, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeGroup, SubscriptionMeasureQuota)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              100,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	// Consume 40 legitimately first so a double release is visible.
	_, err := PreConsumeUserSubscription("waive-keep", 9341, "vip", "m", 0, 40)
	require.NoError(t, err)
	assert.Equal(t, int64(40), reloadSubscriptionCounter(t, counter.Id).Used)

	// Reserve 10, waive it (releases 10), then settle with actual 0.
	_, err = PreConsumeUserSubscription("waive-empty", 9341, "vip", "m", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(50), reloadSubscriptionCounter(t, counter.Id).Used)
	require.NoError(t, WaiveSubscriptionRequestConsume("waive-empty"))
	assert.Equal(t, int64(40), reloadSubscriptionCounter(t, counter.Id).Used)

	funding := &SubscriptionFunding{requestId: "waive-empty", userId: 9341, subscriptionId: sub.Id, counterId: counter.Id}
	require.NoError(t, funding.Settle(-10))
	assert.Equal(t, int64(40), reloadSubscriptionCounter(t, counter.Id).Used,
		"settle after waive must not release the reservation twice")
}

// A replayed pre-consume keeps the entitlement row id so settlement still
// reconciles the row.
func TestReplayKeepsCounterId(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9342, SubscriptionKindTime, []string{"vip"}, nil, 0)
	require.NoError(t, dbx.DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]interface{}{
			"scope":   SubscriptionScopeGroup,
			"measure": SubscriptionMeasureQuota,
		}).Error)
	plan.Scope = SubscriptionScopeGroup
	plan.Measure = SubscriptionMeasureQuota
	sub := activeSubscriptionForKindTest(t, 9342, plan)
	stampSubscriptionAxes(t, sub.Id, SubscriptionScopeGroup, SubscriptionMeasureQuota)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              100,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	first, err := PreConsumeUserSubscription("replay-1", 9342, "vip", "m", 0, 20)
	require.NoError(t, err)
	require.Equal(t, counter.Id, first.RequestCounterId)

	replay, err := PreConsumeUserSubscription("replay-1", 9342, "vip", "m", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, counter.Id, replay.RequestCounterId, "replay must restore the row id")
	assert.Equal(t, int64(20), reloadSubscriptionCounter(t, counter.Id).Used, "replay must not reserve twice")
}
