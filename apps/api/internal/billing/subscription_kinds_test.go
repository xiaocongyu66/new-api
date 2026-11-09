package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Request counters are reserved per request id and released idempotently on
// refund; exhausting a counter must reject further requests.
func TestPreConsumeSubscriptionRequestCountingAndRefund(t *testing.T) {
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

	res, err := PreConsumeSubscriptionRequest("req-count-1", 9302, "vip", "gpt-4o")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(1), res.PreConsumed)
	assert.Equal(t, counter.Id, res.RequestCounterId)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)

	// Same request id reuses the existing reservation instead of counting twice.
	replay, err := PreConsumeSubscriptionRequest("req-count-1", 9302, "vip", "gpt-4o")
	require.NoError(t, err)
	require.NotNil(t, replay)
	assert.Equal(t, int64(1), reloadSubscriptionCounter(t, counter.Id).Used)

	require.NoError(t, RefundSubscriptionRequestConsume("req-count-1"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)
	require.NoError(t, RefundSubscriptionRequestConsume("req-count-1"))
	assert.Equal(t, int64(0), reloadSubscriptionCounter(t, counter.Id).Used)

	_, err = PreConsumeSubscriptionRequest("req-count-2", 9302, "vip", "gpt-4o")
	require.NoError(t, err)
	_, err = PreConsumeSubscriptionRequest("req-count-3", 9302, "vip", "gpt-4o")
	require.NoError(t, err)
	_, err = PreConsumeSubscriptionRequest("req-count-4", 9302, "vip", "gpt-4o")
	require.Error(t, err)
	assert.Equal(t, int64(2), reloadSubscriptionCounter(t, counter.Id).Used)
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
