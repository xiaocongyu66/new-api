package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedGroupedUserForOverlapTest(t *testing.T, id int) {
	t.Helper()
	user := &identity.User{
		Id:       id,
		Username: "overlap-group-user",
		Password: "unused-password-hash",
		Quota:    4000000,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
}

func expireAllUserSubscriptions(t *testing.T, userId int) {
	t.Helper()
	past := common.GetTimestamp() - 100
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ?", userId).
		Update("end_time", past).Error)
}

func userGroupForOverlapTest(t *testing.T, userId int) string {
	t.Helper()
	var user identity.User
	require.NoError(t, dbx.DB.First(&user, userId).Error)
	return user.Group
}

func newUpgradePlanForOverlapTest(t *testing.T, title, group string) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Title:         title,
		Enabled:       true,
		PayMode:       SubscriptionPayModeBalance,
		PriceAmount:   1.0,
		TotalAmount:   100000,
		UpgradeGroup:  group,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, dbx.DB.Create(plan).Error)
	return plan
}

// Overlapping purchases of the same upgrade group record prev_user_group only
// on the elevating row (duplicates store the empty string). When both expire,
// the expiry job must still unwind to the pre-purchase baseline group.
func TestOverlappingSameUpgradeGroupRevertsToBaseline(t *testing.T) {
	setupIdentityTestDB(t)
	seedGroupedUserForOverlapTest(t, 7101)
	plan := newUpgradePlanForOverlapTest(t, "Pro Monthly", "pro")

	require.NoError(t, PurchaseSubscriptionWithWallet(7101, plan.Id, ""))
	require.NoError(t, PurchaseSubscriptionWithWallet(7101, plan.Id, ""))
	require.Equal(t, "pro", userGroupForOverlapTest(t, 7101))

	expireAllUserSubscriptions(t, 7101)
	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 2, expired)
	assert.Equal(t, "default", userGroupForOverlapTest(t, 7101))
}

// Sequential upgrades pro then premium: after both expire the user must land
// on the original baseline, not linger on the intermediate upgraded group.
func TestSequentialDifferentUpgradeGroupsRevertToBaseline(t *testing.T) {
	setupIdentityTestDB(t)
	seedGroupedUserForOverlapTest(t, 7102)
	proPlan := newUpgradePlanForOverlapTest(t, "Pro Plan", "pro")
	premiumPlan := newUpgradePlanForOverlapTest(t, "Premium Plan", "premium")

	require.NoError(t, PurchaseSubscriptionWithWallet(7102, proPlan.Id, ""))
	require.Equal(t, "pro", userGroupForOverlapTest(t, 7102))
	require.NoError(t, PurchaseSubscriptionWithWallet(7102, premiumPlan.Id, ""))
	require.Equal(t, "premium", userGroupForOverlapTest(t, 7102))

	expireAllUserSubscriptions(t, 7102)
	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 2, expired)
	assert.Equal(t, "default", userGroupForOverlapTest(t, 7102))
}

// A group the subscription system never granted (manual admin assignment) is
// the user's own baseline and must survive expiry untouched.
func TestManuallyAssignedGroupSurvivesExpiry(t *testing.T) {
	setupIdentityTestDB(t)
	seedGroupedUserForOverlapTest(t, 7103)
	proPlan := newUpgradePlanForOverlapTest(t, "Pro Plan 7103", "pro")

	require.NoError(t, PurchaseSubscriptionWithWallet(7103, proPlan.Id, ""))
	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", 7103).
		Update("group", "special").Error)

	expireAllUserSubscriptions(t, 7103)
	_, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, "special", userGroupForOverlapTest(t, 7103))
}

// While at least one upgraded subscription is still active, expiry of an
// overlapping one must not downgrade the user.
func TestExpiryWithActiveUpgradeKeepsGroup(t *testing.T) {
	setupIdentityTestDB(t)
	seedGroupedUserForOverlapTest(t, 7104)
	plan := newUpgradePlanForOverlapTest(t, "Pro Monthly 7104", "pro")

	require.NoError(t, PurchaseSubscriptionWithWallet(7104, plan.Id, ""))
	require.NoError(t, PurchaseSubscriptionWithWallet(7104, plan.Id, ""))

	past := common.GetTimestamp() - 100
	require.NoError(t, dbx.DB.Model(&UserSubscription{}).
		Where("user_id = ? AND id = (SELECT MIN(id) FROM user_subscriptions WHERE user_id = ?)", 7104, 7104).
		Update("end_time", past).Error)

	expired, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)
	assert.Equal(t, "pro", userGroupForOverlapTest(t, 7104))
}
