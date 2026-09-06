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

	assert.Equal(t, billing.SubscriptionPayModeNone, billing.NormalizePayMode(billing.SubscriptionPayModeNone, nil))
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode(billing.SubscriptionPayModeBalance, nil))
	assert.Equal(t, billing.SubscriptionPayModeSpore, billing.NormalizePayMode(billing.SubscriptionPayModeSpore, nil))
	assert.Equal(t, billing.SubscriptionPayModeBoth, billing.NormalizePayMode(billing.SubscriptionPayModeBoth, nil))
	assert.Equal(t, billing.SubscriptionPayModeEither, billing.NormalizePayMode(billing.SubscriptionPayModeEither, nil))

	// Fallbacks for empty / legacy mode
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode("", nil))
	assert.Equal(t, billing.SubscriptionPayModeBalance, billing.NormalizePayMode("", common.GetPointer(true)))
	assert.Equal(t, billing.SubscriptionPayModeNone, billing.NormalizePayMode("", common.GetPointer(false)))
}

func TestSubscriptionPlanRequiresCurrency(t *testing.T) {
	t.Parallel()

	planBalance := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeBalance}
	assert.True(t, planBalance.RequiresBalance())
	assert.False(t, planBalance.RequiresSpore())

	planSpore := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeSpore}
	assert.False(t, planSpore.RequiresBalance())
	assert.True(t, planSpore.RequiresSpore())

	planBoth := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeBoth}
	assert.True(t, planBoth.RequiresBalance())
	assert.True(t, planBoth.RequiresSpore())

	planEither := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeEither}
	assert.False(t, planEither.RequiresBalance())
	assert.False(t, planEither.RequiresSpore())

	planNone := &billing.SubscriptionPlan{PayMode: billing.SubscriptionPayModeNone}
	assert.False(t, planNone.RequiresBalance())
	assert.False(t, planNone.RequiresSpore())
}

func TestPurchaseSubscriptionWithWallet_Scenarios(t *testing.T) {
	// Create test user: 1000000 quota ($2.00 at 500k/unit), 30 spore units (3.0 spore)
	user := &identity.User{
		Username: "sub-wallet-user",
		Password: "password123",
		Quota:    1000000,
		Spore:    30,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	// Plan 1: Free (mode none)
	planFree := &billing.SubscriptionPlan{
		Title:        "Free Plan",
		Enabled:      true,
		PayMode:      billing.SubscriptionPayModeNone,
		PriceAmount:  0,
		SporeAmount:  0,
		TotalAmount:  100000,
		DurationUnit: "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planFree).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planFree.Id, ""))

	// User quota and spore untouched
	refreshed, err := identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 1000000, refreshed.Quota)
	assert.Equal(t, int64(30), refreshed.Spore)

	// Plan 2: Spore only (costs 1.5 spore = 15 units)
	planSpore := &billing.SubscriptionPlan{
		Title:        "Spore Plan",
		Enabled:      true,
		PayMode:      billing.SubscriptionPayModeSpore,
		PriceAmount:  0,
		SporeAmount:  15,
		TotalAmount:  100000,
		DurationUnit: "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planSpore).Error)
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planSpore.Id, ""))

	// Spore deducted by 15, quota untouched
	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, int64(15), refreshed.Spore)
	assert.Equal(t, 1000000, refreshed.Quota)

	// Plan 3: Insufficient spore fails atomically
	planExpensiveSpore := &billing.SubscriptionPlan{
		Title:        "Expensive Spore Plan",
		Enabled:      true,
		PayMode:      billing.SubscriptionPayModeSpore,
		SporeAmount:  50, // user only has 15
		TotalAmount:  100000,
		DurationUnit: "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planExpensiveSpore).Error)
	err = billing.PurchaseSubscriptionWithWallet(user.Id, planExpensiveSpore.Id, "")
	assert.ErrorIs(t, err, identity.ErrSporeInsufficient)

	// Plan 4: Either balance or spore (balance = $1 = 500000 quota, spore = 1.0 = 10 units)
	planEither := &billing.SubscriptionPlan{
		Title:        "Either Plan",
		Enabled:      true,
		PayMode:      billing.SubscriptionPayModeEither,
		PriceAmount:  1.0,
		SporeAmount:  10,
		TotalAmount:  100000,
		DurationUnit: "month",
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(planEither).Error)

	// User chooses spore payment
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planEither.Id, billing.SubscriptionPayModeSpore))
	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, int64(5), refreshed.Spore)  // 15 - 10 = 5
	assert.Equal(t, 1000000, refreshed.Quota)  // quota untouched

	// User purchases again choosing balance payment
	require.NoError(t, billing.PurchaseSubscriptionWithWallet(user.Id, planEither.Id, billing.SubscriptionPayModeBalance))
	refreshed, err = identity.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, int64(5), refreshed.Spore) // spore untouched
	assert.Equal(t, 500000, refreshed.Quota)  // 1000000 - 500000 = 500000
}
