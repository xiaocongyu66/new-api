package billing_test

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePayMode(t *testing.T) {
	t.Parallel()

	// Current modes pass through untouched.
	assert.Equal(t, billing.SubscriptionPayModeNone, billing.NormalizePayMode(billing.SubscriptionPayModeNone, nil))
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode(billing.SubscriptionPayModeBalance, nil))

	// Rows that used to mix in a second settlement currency have no settlement
	// path left, so they fold back onto balance instead of degrading a paid plan
	// into a free one. Dirty values take the same route.
	for _, legacy := range []string{"both", "either", "bogus"} {
		assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode(legacy, nil), "mode %q", legacy)
	}

	// Rows predating pay_mode carry no mode at all, so AllowBalancePay decides.
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode("", nil))
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode("", common.GetPointer(true)))
	assert.Equal(t, billing.SubscriptionPayModeNone, billing.NormalizePayMode("", common.GetPointer(false)))
}

func TestSubscriptionPlanRequiresBalance(t *testing.T) {
	t.Parallel()

	planBalance := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeBalance}
	assert.True(t, planBalance.RequiresBalance())

	planNone := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeNone}
	assert.False(t, planNone.RequiresBalance())

	// Legacy modes that once mixed in a second currency settle from balance.
	assert.True(t, (&billing.SubscriptionPlan{PayMode: "both"}).RequiresBalance())
	assert.True(t, (&billing.SubscriptionPlan{PayMode: "either"}).RequiresBalance())
}

func TestPurchaseSubscriptionWithWallet_Scenarios(t *testing.T) {
	// Test user: 1000000 quota ($2.00 at 500k/unit)
	user := &identity.User{
		Username: "sub-wallet-user",
		Password: "password123",
		Quota:    1000000,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	// Plan 1: free (mode none) — purchase succeeds, quota untouched
	planFree := &billing.SubscriptionPlan{
		Title:         "Free Plan",
		Enabled:       true,
		PayMode:       billing.SubscriptionPayModeNone,
		TotalAmount:   100000,
		DurationUnit:  "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planFree).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planFree.Id))

	refreshed, err := identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 1000000, refreshed.Quota)

	// Plan 2: balance plan costs $1 = 500000 quota
	planBalance := &billing.SubscriptionPlan{
		Title:         "Balance Plan",
		Enabled:       true,
		PayMode:       billing.SubscriptionPayModeBalance,
		PriceAmount:   1.0,
		TotalAmount:   100000,
		DurationUnit:  "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planBalance).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planBalance.Id))

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 500000, refreshed.Quota)

	// Plan 3: a legacy row that once mixed in a second currency settles from
	// balance alone ($0.5 = 250000 quota)
	planLegacy := &billing.SubscriptionPlan{
		Title:         "Legacy Plan",
		Enabled:       true,
		PayMode:       "either",
		PriceAmount:   0.5,
		TotalAmount:   100000,
		DurationUnit:  "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planLegacy).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planLegacy.Id))

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 250000, refreshed.Quota)

	// Plan 4: too expensive for the remaining quota — the purchase fails and
	// nothing is charged
	planExpensive := &billing.SubscriptionPlan{
		Title:         "Expensive Plan",
		Enabled:       true,
		PayMode:       billing.SubscriptionPayModeBalance,
		PriceAmount:   5.0,
		TotalAmount:   100000,
		DurationUnit:  "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planExpensive).Error)
	err = billing.PurchaseSubscriptionWithWallet(user.Id, planExpensive.Id)
	assert.Error(t, err)

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 250000, refreshed.Quota)
}
