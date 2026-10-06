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

func TestPurchaseSubscriptionWithWallet_Scenarios(t *testing.T) {
	// Test user: 1000000 quota ($2.00 at 500k/unit)
	user := &identity.User{
		Username: "sub-wallet-user",
		Password: "password123",
		Quota:    1000000,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	// Free plan (price 0): purchase succeeds, quota untouched.
	planFree := &billing.SubscriptionPlan{
		Title:           "Free Plan",
		Enabled:         true,
		AllowBalancePay: common.GetPointer(true),
		PriceAmount:     0,
		TotalAmount:     100000,
		DurationUnit:    "month",
		DurationValue:   1,
	}
	require.NoError(t, dbx.DB.Create(planFree).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planFree.Id))

	refreshed, err := identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 1000000, refreshed.Quota)

	// Balance plan costs $1 = 500000 quota.
	planBalance := &billing.SubscriptionPlan{
		Title:           "Balance Plan",
		Enabled:         true,
		AllowBalancePay: common.GetPointer(true),
		PriceAmount:     1.0,
		TotalAmount:     100000,
		DurationUnit:    "month",
		DurationValue:   1,
	}
	require.NoError(t, dbx.DB.Create(planBalance).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planBalance.Id))

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 500000, refreshed.Quota)

	// Third-party-only plan (allow_balance_pay=false) cannot be settled from the
	// wallet: the balance purchase is rejected and nothing is charged.
	planNoBalance := &billing.SubscriptionPlan{
		Title:           "No-Balance Plan",
		Enabled:         true,
		AllowBalancePay: common.GetPointer(false),
		PriceAmount:     0.5,
		TotalAmount:     100000,
		DurationUnit:    "month",
		DurationValue:   1,
	}
	require.NoError(t, dbx.DB.Create(planNoBalance).Error)
	err = billing.PurchaseSubscriptionWithWallet(user.Id, planNoBalance.Id)
	assert.Error(t, err)

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 500000, refreshed.Quota)

	// Too expensive for the remaining quota — the purchase fails, nothing charged.
	planExpensive := &billing.SubscriptionPlan{
		Title:           "Expensive Plan",
		Enabled:         true,
		AllowBalancePay: common.GetPointer(true),
		PriceAmount:     5.0,
		TotalAmount:     100000,
		DurationUnit:    "month",
		DurationValue:   1,
	}
	require.NoError(t, dbx.DB.Create(planExpensive).Error)
	err = billing.PurchaseSubscriptionWithWallet(user.Id, planExpensive.Id)
	assert.Error(t, err)

	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 500000, refreshed.Quota)
}
