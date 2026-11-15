package billing

import (
	"fmt"
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedConversionRatios(t *testing.T) {
	t.Helper()
	saved := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(saved)
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2,"尝尝？":1}`))
}

func seedConversionUser(t *testing.T, id int, group string) {
	t.Helper()
	require.NoError(t, dbx.DB.Create(&identity.User{
		Id:       id,
		Username: fmt.Sprintf("conv_user_%d", id),
		AffCode:  fmt.Sprintf("conv_aff_%d", id),
		Quota:    1_000_000,
		Status:   common.UserStatusEnabled,
		Group:    group,
	}).Error)
}

func seedLegacySharedPlan(t *testing.T, id int, title string, kind string, upgradeGroup string, total int64, reset string) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:               id,
		Title:            title,
		Enabled:          true,
		AllowBalancePay:  common.GetPointer(true),
		PriceAmount:      0,
		Kind:             kind,
		TotalAmount:      total,
		QuotaResetPeriod: reset,
		UpgradeGroup:     upgradeGroup,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)
	return plan
}

func seedLegacySharedSubscription(t *testing.T, id int, userId int, planId int, kind string, total int64, used int64, upgradeGroup string, upgradeGroupsJSON string, endTime int64) *UserSubscription {
	t.Helper()
	sub := &UserSubscription{
		Id:                id,
		UserId:            userId,
		PlanId:            planId,
		Kind:              kind,
		AmountTotal:       total,
		AmountUsed:        used,
		StartTime:         common.GetTimestamp() - 60,
		EndTime:           endTime,
		Status:            "active",
		Source:            "order",
		UpgradeGroup:      upgradeGroup,
		UpgradeGroupsJSON: upgradeGroupsJSON,
	}
	require.NoError(t, dbx.DB.Create(sub).Error)
	return sub
}

func reloadCounterForGroup(t *testing.T, subId int, group string) SubscriptionRequestCounter {
	t.Helper()
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.
		Where("user_subscription_id = ? AND group_name = ? AND model_name = ?", subId, group, "").
		First(&counter).Error)
	return counter
}

// The production shape: plans 1/2/3 are legacy shared pools (no explicit
// scope), one plan has no upgrade group at all, and the active subscriptions
// carry scope=” or scope='all'. The conversion must move the money, keep the
// current group usable, and skip rows it cannot express.
func TestConvertLegacySharedScopesRewritesPlansAndSubscriptions(t *testing.T) {
	setupIdentityTestDB(t)
	seedConversionRatios(t)

	seedLegacySharedPlan(t, 1, "trial", SubscriptionKindTime, "", 500_000, SubscriptionResetNever)
	seedLegacySharedPlan(t, 2, "vip", SubscriptionKindTime, "vip", 5_500_000, SubscriptionResetNever)
	seedLegacySharedPlan(t, 3, "unlimited", SubscriptionKindTime, "vip", 0, SubscriptionResetNever)
	seedLegacySharedPlan(t, 4, "reset", SubscriptionKindTime, "vip", 1_000, SubscriptionResetMonthly)

	seedConversionUser(t, 1, "default")
	seedConversionUser(t, 2, "vip")
	seedConversionUser(t, 3, "尝尝？")

	now := common.GetTimestamp()
	seedLegacySharedSubscription(t, 1, 1, 1, SubscriptionKindTime, 500_000, 100_000, "", "", now+3600)
	seedLegacySharedSubscription(t, 2, 2, 2, SubscriptionKindTime, 5_500_000, 1_000_000, "vip", marshalStringListJSON([]string{"vip"}), now+3600)
	seedLegacySharedSubscription(t, 3, 3, 1, SubscriptionKindTime, 500_000, 0, "", "", now+3600)

	report, err := ConvertLegacySharedScopes(SharedScopeConversionRequest{DefaultGroup: "default"})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.DryRun)
	// Plan 1 has no upgrade group and plans 3/4 cannot be expressed as a group
	// pool; only plan 2 converts on the default pass. Subscriptions still move
	// because each one lands on the user's current group.
	require.Len(t, report.Plans, 1)
	require.Len(t, report.Subscriptions, 3)
	require.Len(t, report.Skipped, 3)

	var trial SubscriptionPlan
	require.NoError(t, dbx.DB.First(&trial, 1).Error)
	assert.Equal(t, SubscriptionScopeAll, trial.Scope)
	assert.Equal(t, int64(500_000), trial.TotalAmount)
	assert.Empty(t, trial.RequestRules)

	var vipPlan SubscriptionPlan
	require.NoError(t, dbx.DB.First(&vipPlan, 2).Error)
	require.Len(t, vipPlan.RequestRules, 1)
	assert.Equal(t, "vip", vipPlan.RequestRules[0].Group)
	assert.Equal(t, int64(5_500_000), vipPlan.RequestRules[0].Amount)

	var unlimited SubscriptionPlan
	require.NoError(t, dbx.DB.First(&unlimited, 3).Error)
	assert.Equal(t, int64(0), unlimited.TotalAmount)
	assert.Empty(t, unlimited.RequestRules)

	var resetPlan SubscriptionPlan
	require.NoError(t, dbx.DB.First(&resetPlan, 4).Error)
	assert.Equal(t, SubscriptionResetMonthly, resetPlan.QuotaResetPeriod)
	assert.Empty(t, resetPlan.RequestRules)

	var trialSub UserSubscription
	require.NoError(t, dbx.DB.First(&trialSub, 1).Error)
	assert.Equal(t, SubscriptionScopeGroup, trialSub.Scope)
	assert.Equal(t, SubscriptionMeasureQuota, trialSub.Measure)
	assert.Equal(t, int64(0), trialSub.AmountTotal)
	assert.Equal(t, int64(0), trialSub.AmountUsed)
	assert.Equal(t, []string{"default"}, trialSub.UpgradeGroups)
	trialCounter := reloadCounterForGroup(t, 1, "default")
	assert.Equal(t, SubscriptionMeasureQuota, trialCounter.Measure)
	assert.Equal(t, int64(500_000), trialCounter.Total)
	assert.Equal(t, int64(100_000), trialCounter.Used)

	var vipSub UserSubscription
	require.NoError(t, dbx.DB.First(&vipSub, 2).Error)
	vipCounter := reloadCounterForGroup(t, 2, "vip")
	assert.Equal(t, int64(5_500_000), vipCounter.Total)
	assert.Equal(t, int64(1_000_000), vipCounter.Used)

	// The third subscription belonged to a plan without an upgrade group; it
	// lands on the user's current group, not the plan fallback.
	var thirdSub UserSubscription
	require.NoError(t, dbx.DB.First(&thirdSub, 3).Error)
	thirdCounter := reloadCounterForGroup(t, 3, "尝尝？")
	assert.Equal(t, int64(500_000), thirdCounter.Total)
	assert.Equal(t, int64(0), thirdCounter.Used)

	// The operator pins plan 1 to a group explicitly; the second pass converts
	// the plan without touching the already-converted subscriptions.
	mapped, err := ConvertLegacySharedScopes(SharedScopeConversionRequest{
		DefaultGroup: "default",
		PlanGroups:   map[int]string{1: "default"},
	})
	require.NoError(t, err)
	require.Len(t, mapped.Plans, 1)
	assert.Empty(t, mapped.Subscriptions)
	require.NoError(t, dbx.DB.First(&trial, 1).Error)
	assert.Equal(t, SubscriptionScopeGroup, trial.Scope)
	assert.Equal(t, int64(0), trial.TotalAmount)
	assert.Equal(t, "default", trial.UpgradeGroup)
	assert.Equal(t, []string{"default"}, trial.UpgradeGroups)
	require.Len(t, trial.RequestRules, 1)
	assert.Equal(t, "default", trial.RequestRules[0].Group)
	assert.Equal(t, int64(500_000), trial.RequestRules[0].Amount)

	// Plans 3/4 stay intentionally skipped; no subscription is rewritten twice.
	again, err := ConvertLegacySharedScopes(SharedScopeConversionRequest{DefaultGroup: "default", Force: true})
	require.NoError(t, err)
	assert.Empty(t, again.Plans)
	assert.Empty(t, again.Subscriptions)
	assert.Len(t, again.Skipped, 2)
}

func TestConvertLegacySharedScopesDryRunDoesNotWrite(t *testing.T) {
	setupIdentityTestDB(t)
	seedConversionRatios(t)
	seedLegacySharedPlan(t, 10, "dry", SubscriptionKindTime, "vip", 1_000, SubscriptionResetNever)
	seedConversionUser(t, 10, "vip")
	seedLegacySharedSubscription(t, 10, 10, 10, SubscriptionKindTime, 1_000, 100, "vip", "", common.GetTimestamp()+3600)

	report, err := ConvertLegacySharedScopes(SharedScopeConversionRequest{DryRun: true, DefaultGroup: "default"})
	require.NoError(t, err)
	require.True(t, report.DryRun)
	require.Len(t, report.Plans, 1)
	require.Len(t, report.Subscriptions, 1)

	var plan SubscriptionPlan
	require.NoError(t, dbx.DB.First(&plan, 10).Error)
	assert.Equal(t, int64(1_000), plan.TotalAmount)
	assert.Empty(t, plan.RequestRules)

	var sub UserSubscription
	require.NoError(t, dbx.DB.First(&sub, 10).Error)
	assert.Equal(t, int64(1_000), sub.AmountTotal)
	var count int64
	require.NoError(t, dbx.DB.Model(&SubscriptionRequestCounter{}).
		Where("user_subscription_id = ?", 10).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestConvertedGroupScopeBillsInTargetGroup(t *testing.T) {
	setupIdentityTestDB(t)
	seedConversionRatios(t)
	seedLegacySharedPlan(t, 20, "trial", SubscriptionKindTime, "", 500_000, SubscriptionResetNever)
	seedConversionUser(t, 20, "default")
	seedLegacySharedSubscription(t, 20, 20, 20, SubscriptionKindTime, 500_000, 100_000, "", "", common.GetTimestamp()+3600)

	_, err := ConvertLegacySharedScopes(SharedScopeConversionRequest{DefaultGroup: "default"})
	require.NoError(t, err)

	res, err := PreConsumeUserSubscription("req-converted-1", 20, "default", "gpt-4o", 0, 1_000)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(500_000), res.AmountTotal)
	assert.Equal(t, int64(101_000), res.AmountUsedAfter)
	assert.Equal(t, int64(500_000), res.CounterTotal)
	assert.Equal(t, int64(101_000), res.CounterUsedAfter)
	assert.Equal(t, int64(101_000), reloadCounterForGroup(t, 20, "default").Used)

	// The counter only exists for the chosen group; another group has no row.
	_, err = PreConsumeUserSubscription("req-converted-2", 20, "vip", "gpt-4o", 0, 1_000)
	require.Error(t, err)

	require.NoError(t, RefundSubscriptionPreConsume("req-converted-1"))
	assert.Equal(t, int64(100_000), reloadCounterForGroup(t, 20, "default").Used)
}

func TestConvertedGroupScopeExpiryHonorsTimeAxis(t *testing.T) {
	setupIdentityTestDB(t)
	now := common.GetTimestamp()
	sub := &UserSubscription{
		Id:        30,
		UserId:    30,
		PlanId:    30,
		Kind:      SubscriptionKindTime,
		Scope:     SubscriptionScopeGroup,
		Measure:   SubscriptionMeasureQuota,
		StartTime: now - 60,
		EndTime:   now + 3600,
		Status:    "active",
	}
	require.NoError(t, dbx.DB.Create(sub).Error)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: 30,
		GroupName:          "default",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              100,
		Used:               100,
	}).Error)

	// A time-expiry quota row is spent, but the term is still running: the row
	// is a cap, not an expiry trigger.
	expired, err := subscriptionReachedEndTx(dbx.DB, sub, now)
	require.NoError(t, err)
	assert.False(t, expired)

	// A quota-expiry row ends as soon as the row is spent.
	quotaSub := *sub
	quotaSub.Kind = SubscriptionKindQuota
	expired, err = subscriptionReachedEndTx(dbx.DB, &quotaSub, now)
	require.NoError(t, err)
	assert.True(t, expired)

	// A zero-total quota row is unlimited and never exhausts.
	unlimited := &UserSubscription{
		Id:        31,
		UserId:    31,
		PlanId:    31,
		Kind:      SubscriptionKindQuota,
		Scope:     SubscriptionScopeGroup,
		Measure:   SubscriptionMeasureQuota,
		StartTime: now - 60,
		Status:    "active",
	}
	require.NoError(t, dbx.DB.Create(unlimited).Error)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: 31,
		GroupName:          "default",
		ModelName:          "",
		Measure:            SubscriptionMeasureQuota,
		Total:              0,
		Used:               0,
	}).Error)
	expired, err = subscriptionReachedEndTx(dbx.DB, unlimited, now)
	require.NoError(t, err)
	assert.False(t, expired)
}
