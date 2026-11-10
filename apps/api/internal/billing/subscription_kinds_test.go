package billing

import (
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedSubscriptionKindPlan(t *testing.T, id int, kind string, groups []string, rules []SubscriptionRequestRule, total int64) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:                id,
		Title:             "kind-plan",
		Enabled:           true,
		AllowBalancePay:   common.GetPointer(true),
		PriceAmount:       0,
		Kind:              kind,
		TotalAmount:       total,
		UpgradeGroups:     groups,
		UpgradeGroupsJSON: marshalStringListJSON(groups),
		RequestRules:      rules,
		RequestRulesJSON:  marshalRequestRulesJSON(rules),
		DurationUnit:      SubscriptionDurationMonth,
		DurationValue:     1,
	}
	if len(groups) > 0 {
		plan.UpgradeGroup = groups[0]
	}
	require.NoError(t, dbx.DB.Create(plan).Error)
	return plan
}

func seedActiveUserSubscription(t *testing.T, sub *UserSubscription) *UserSubscription {
	t.Helper()
	require.NoError(t, dbx.DB.Create(sub).Error)
	return sub
}

func activeSubscriptionForKindTest(t *testing.T, userId int, plan *SubscriptionPlan) *UserSubscription {
	t.Helper()
	now := common.GetTimestamp()
	sub := &UserSubscription{
		UserId:            userId,
		PlanId:            plan.Id,
		Kind:              plan.Kind,
		AmountTotal:       plan.TotalAmount,
		StartTime:         now - 60,
		Status:            "active",
		Source:            "order",
		UpgradeGroup:      plan.UpgradeGroup,
		UpgradeGroupsJSON: plan.UpgradeGroupsJSON,
	}
	if plan.Kind == SubscriptionKindTime {
		sub.EndTime = now + 3600
	}
	require.NoError(t, dbx.DB.Create(sub).Error)
	return sub
}

func reloadSubscriptionCounter(t *testing.T, id int) SubscriptionRequestCounter {
	t.Helper()
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.First(&counter, id).Error)
	return counter
}

// A subscription scoped to an upgraded group must not pay for requests served
// through another group.
func TestPreConsumeUserSubscriptionRejectsGroupMismatch(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9301, SubscriptionKindTime, []string{"vip"}, nil, 1000)
	sub := activeSubscriptionForKindTest(t, 9301, plan)

	_, err := PreConsumeUserSubscription("req-group-mismatch", 9301, "default", "gpt-4o", 0, 100)
	require.Error(t, err)
	var reloaded UserSubscription
	require.NoError(t, dbx.DB.First(&reloaded, sub.Id).Error)
	assert.Equal(t, int64(0), reloaded.AmountUsed)

	res, err := PreConsumeUserSubscription("req-group-match", 9301, "vip", "gpt-4o", 0, 100)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NoError(t, dbx.DB.First(&reloaded, sub.Id).Error)
	assert.Equal(t, int64(100), reloaded.AmountUsed)
}

// Request-kind subscriptions reserve one request from the matching counter and
// then consume standard subscription quota; refunds release both legs.
func TestPreConsumeUserSubscriptionRequestKindCountingAndRefund(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9302, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 2}},
		0)
	sub := activeSubscriptionForKindTest(t, 9302, plan)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "gpt-4o",
		Total:              2,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	res, err := PreConsumeUserSubscription("req-count-1", 9302, "vip", "gpt-4o", 0, 100)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(100), res.PreConsumed)
	assert.Equal(t, int64(0), res.RequestCounterUsedBefore)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)

	// Same request id replays without counting or charging twice.
	replay, err := PreConsumeUserSubscription("req-count-1", 9302, "vip", "gpt-4o", 0, 100)
	require.NoError(t, err)
	require.NotNil(t, replay)
	assert.Equal(t, int64(100), replay.PreConsumed)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)

	var usedAfterPre UserSubscription
	require.NoError(t, dbx.DB.First(&usedAfterPre, sub.Id).Error)
	assert.Equal(t, int64(100), usedAfterPre.AmountUsed)

	// Refund releases both the counter and the reserved quota.
	require.NoError(t, RefundSubscriptionPreConsume("req-count-1"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
	require.NoError(t, RefundSubscriptionPreConsume("req-count-1"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
	var usedAfterRefund UserSubscription
	require.NoError(t, dbx.DB.First(&usedAfterRefund, sub.Id).Error)
	assert.Equal(t, int64(0), usedAfterRefund.AmountUsed)

	// Exhaustion gates further requests even though the quota pool is unlimited.
	_, err = PreConsumeUserSubscription("req-count-2", 9302, "vip", "gpt-4o", 0, 10)
	require.NoError(t, err)
	_, err = PreConsumeUserSubscription("req-count-3", 9302, "vip", "gpt-4o", 0, 10)
	require.NoError(t, err)
	_, err = PreConsumeUserSubscription("req-count-4", 9302, "vip", "gpt-4o", 0, 10)
	require.Error(t, err)
	assert.Equal(t, int64(2), reloadSubscriptionCounter(t, counter.Id).Used)
}

// Purchase-time budget snapshot: count x model price x group ratio.
func TestCreateSubscriptionRequestCountersSnapshotsQuotaBudget(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9305, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 10}},
		0)
	modelPrice, priced := ratio_setting.GetModelPrice("gpt-4o", false)
	groupRatio := ratio_setting.GetGroupRatio("vip")
	require.True(t, priced && modelPrice > 0 && groupRatio > 0, "test fixture needs a priced model in group vip")

	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, 9305, plan, "test")
		return err
	}))
	var counters []SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = (SELECT max(id) FROM user_subscriptions)").Find(&counters).Error)
	require.Len(t, counters, 1)

	want := int64(common.QuotaFromFloat(10 * modelPrice * groupRatio * common.QuotaPerUnit))
	assert.Equal(t, want, counters[0].QuotaTotal)
	assert.Equal(t, int64(0), counters[0].QuotaUsed)
}

// Waving an empty/short-cut release must release the counter idempotently.
func TestWaiveSubscriptionRequestConsumeReleasesCounterIdempotently(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9306, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 2}},
		0)
	sub := activeSubscriptionForKindTest(t, 9306, plan)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "gpt-4o",
		Total:              2,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	_, err := PreConsumeUserSubscription("req-waive", 9306, "vip", "gpt-4o", 0, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)

	require.NoError(t, WaiveSubscriptionRequestConsume("req-waive"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
	// Replay must not release a second time (idempotent).
	require.NoError(t, WaiveSubscriptionRequestConsume("req-waive"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)

	// Refund after a counted request releases the counter and quota once.
	_, err = PreConsumeUserSubscription("req-waive-retry", 9306, "vip", "gpt-4o", 0, 50)
	require.NoError(t, err)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)
	require.NoError(t, RefundSubscriptionPreConsume("req-waive-retry"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
	var afterRefund UserSubscription
	require.NoError(t, dbx.DB.First(&afterRefund, sub.Id).Error)
	assert.Equal(t, int64(0), afterRefund.AmountUsed)
}

// Settlement attribution: settled quota mirrors onto counter.quota_used, and
// a negative delta (waived settlement) removes it again.
func TestAttributeSubscriptionCounterQuotaMirrorIsIdempotent(t *testing.T) {
	setupIdentityTestDB(t)
	plan := seedSubscriptionKindPlan(t, 9307, SubscriptionKindRequest,
		[]string{"vip"},
		[]SubscriptionRequestRule{{Group: "vip", Models: []string{"gpt-4o"}, Count: 2}},
		0)
	sub := activeSubscriptionForKindTest(t, 9307, plan)
	require.NoError(t, dbx.DB.Create(&SubscriptionRequestCounter{
		UserSubscriptionId: sub.Id,
		GroupName:          "vip",
		ModelName:          "gpt-4o",
		Total:              2,
	}).Error)
	var counter SubscriptionRequestCounter
	require.NoError(t, dbx.DB.Where("user_subscription_id = ?", sub.Id).First(&counter).Error)

	_, err := PreConsumeUserSubscription("req-attr", 9307, "vip", "gpt-4o", 0, 100)
	require.NoError(t, err)
	require.NoError(t, AttributeSubscriptionCounterQuota("req-attr", 100))
	assert.Equal(t, int64(100), reloadSubscriptionCounter(t, counter.Id).QuotaUsed)

	// Over-settle (e.g. a usage-corrected delta) accumulates rather than
	// replaces, mirroring the subscription's amount_used semantics.
	require.NoError(t, AttributeSubscriptionCounterQuota("req-attr", 50))
	assert.Equal(t, int64(150), reloadSubscriptionCounter(t, counter.Id).QuotaUsed)

	// Refund/waive un-attributes back to zero and never below.
	require.NoError(t, AttributeSubscriptionCounterQuota("req-attr", -150))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).QuotaUsed)
	require.NoError(t, AttributeSubscriptionCounterQuota("req-attr", -10))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).QuotaUsed)

	// Unknown request ids are silent no-ops.
	require.NoError(t, AttributeSubscriptionCounterQuota("req-does-not-exist", 5))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).QuotaUsed)
}

// A quota-kind subscription expires when its pool is spent even though it has
// no end_time, while a partially used one stays active.
func TestExpireDueSubscriptionsExpiresExhaustedQuotaKind(t *testing.T) {
	setupIdentityTestDB(t)
	exhaustedPlan := seedSubscriptionKindPlan(t, 9303, SubscriptionKindQuota, nil, nil, 100)
	exhausted := activeSubscriptionForKindTest(t, 9303, exhaustedPlan)
	exhausted.AmountUsed = 100
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).Where("id = ?", exhausted.Id).Update("amount_used", 100).Error)

	remainingPlan := seedSubscriptionKindPlan(t, 9304, SubscriptionKindQuota, nil, nil, 100)
	remaining := activeSubscriptionForKindTest(t, 9304, remainingPlan)
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).Where("id = ?", remaining.Id).Update("amount_used", 30).Error)

	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)

	var reloadedExhausted UserSubscription
	require.NoError(t, dbx.DB.First(&reloadedExhausted, exhausted.Id).Error)
	assert.Equal(t, "expired", reloadedExhausted.Status)
	var reloadedRemaining UserSubscription
	require.NoError(t, dbx.DB.First(&reloadedRemaining, remaining.Id).Error)
	assert.Equal(t, "active", reloadedRemaining.Status)
}
