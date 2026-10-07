package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/require"
)

// TestSubscriptionSettleChargesOverageAsWalletDebt pins the F8 invariant:
// a post-delivery settlement delta larger than the subscription's remaining
// quota must consume the subscription to its cap AND charge the remainder to
// the wallet (debt-allowed, mirroring WalletFunding.Settle). Before the fix
// the strict all-or-nothing delta failed, the settle error was logged away,
// and the delivered overage was billed to nobody.
func TestSubscriptionSettleChargesOverageAsWalletDebt(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 91, 700)
	require.NoError(t, dbx.DB.Create(&UserSubscription{
		Id: 93, UserId: 91, AmountTotal: 1000, AmountUsed: 900, Status: "active",
	}).Error)

	funding := &SubscriptionFunding{userId: 91, subscriptionId: 93}
	require.NoError(t, funding.Settle(200))

	var used int64
	require.NoError(t, dbx.DB.Raw("SELECT amount_used FROM user_subscriptions WHERE id = ?", 93).Scan(&used).Error)
	require.Equal(t, int64(1000), used, "subscription absorbs everything up to its cap")
	require.Equal(t, 700-100, scanUserQuota(t), "the 100-unit overflow is charged to the wallet")
}

// TestSubscriptionSettleWithinRemainingQuotaLeavesWalletUntouched guards the
// happy path of the capped settle: no cap hit, wallet untouched.
func TestSubscriptionSettleWithinRemainingQuotaLeavesWalletUntouched(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 91, 700)
	require.NoError(t, dbx.DB.Create(&UserSubscription{
		Id: 93, UserId: 91, AmountTotal: 1000, AmountUsed: 100, Status: "active",
	}).Error)

	funding := &SubscriptionFunding{userId: 91, subscriptionId: 93}
	require.NoError(t, funding.Settle(200))

	var used int64
	require.NoError(t, dbx.DB.Raw("SELECT amount_used FROM user_subscriptions WHERE id = ?", 93).Scan(&used).Error)
	require.Equal(t, int64(300), used)
	require.Equal(t, 700, scanUserQuota(t))
}

// TestSubscriptionSettleNegativeDeltaRefunds keeps the refund leg on the
// strict delta path (floor at zero), so settle adjustments can still give
// quota back without touching the wallet.
func TestSubscriptionSettleNegativeDeltaRefunds(t *testing.T) {
	setupIdentityTestDB(t)
	seedUser(t, 91, 700)
	require.NoError(t, dbx.DB.Create(&UserSubscription{
		Id: 93, UserId: 91, AmountTotal: 1000, AmountUsed: 300, Status: "active",
	}).Error)

	funding := &SubscriptionFunding{userId: 91, subscriptionId: 93}
	require.NoError(t, funding.Settle(-500))

	var used int64
	require.NoError(t, dbx.DB.Raw("SELECT amount_used FROM user_subscriptions WHERE id = ?", 93).Scan(&used).Error)
	require.Equal(t, int64(0), used)
	require.Equal(t, 700, scanUserQuota(t))
}
