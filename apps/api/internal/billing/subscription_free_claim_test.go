package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Zero-cost plans that grant quota or upgrade-group entitlement through the
// self-service wallet path must carry a finite per-user purchase cap,
// otherwise every account can claim unlimited full-value subscriptions.

func TestFreeClaimPlanWithoutCapIsRejected(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 6101, 0)

	plan := &SubscriptionPlan{
		Title:         "Uncapped Free",
		Enabled:       true,
		PayMode:       SubscriptionPayModeBalance,
		PriceAmount:   0,
		TotalAmount:   100000,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)

	err := PurchaseSubscriptionWithWallet(6101, plan.Id, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "购买上限")

	var count int64
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 6101, plan.Id).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestCappedFreeClaimSucceedsOnceOnly(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 6102, 0)

	plan := &SubscriptionPlan{
		Title:              "Capped Free",
		Enabled:            true,
		PayMode:            SubscriptionPayModeNone,
		TotalAmount:        100000,
		DurationUnit:       SubscriptionDurationMonth,
		DurationValue:      1,
		MaxPurchasePerUser: 1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)

	require.NoError(t, PurchaseSubscriptionWithWallet(6102, plan.Id, ""))

	err := PurchaseSubscriptionWithWallet(6102, plan.Id, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "购买上限")

	var user identity.User
	require.NoError(t, dbx.DB.First(&user, 6102).Error)
	assert.Equal(t, 0, user.Quota)
}

func TestPaidPlanInNonePayModeCannotBeClaimedFree(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 6103, 1000000)

	plan := &SubscriptionPlan{
		Title:         "Gateway Only",
		Enabled:       true,
		PayMode:       SubscriptionPayModeNone,
		PriceAmount:   9.9,
		TotalAmount:   100000,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)

	err := PurchaseSubscriptionWithWallet(6103, plan.Id, "")
	require.Error(t, err)

	var count int64
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 6103, plan.Id).Count(&count).Error)
	assert.Equal(t, int64(0), count)

	var user identity.User
	require.NoError(t, dbx.DB.First(&user, 6103).Error)
	assert.Equal(t, 1000000, user.Quota)
}

func TestZeroValueFreePlanStaysUncapped(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 6104, 0)

	plan := &SubscriptionPlan{
		Title:         "Placeholder",
		Enabled:       true,
		PayMode:       SubscriptionPayModeNone,
		TotalAmount:   0,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)

	require.NoError(t, PurchaseSubscriptionWithWallet(6104, plan.Id, ""))
	require.NoError(t, PurchaseSubscriptionWithWallet(6104, plan.Id, ""))

	var count int64
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 6104, plan.Id).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
