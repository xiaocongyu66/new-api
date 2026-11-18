package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A shared pool is one pool for every group, so the group snapshot must not
// gate it; only scoped entitlement rows are group-bound.
func TestSubscriptionCoversGroup(t *testing.T) {
	legacyShared := &UserSubscription{Kind: SubscriptionKindTime, UpgradeGroup: "vip"}
	assert.True(t, subscriptionCoversGroup(legacyShared, "default"),
		"a legacy shared pool keeps serving every group")
	assert.True(t, subscriptionCoversGroup(legacyShared, "清水rp"))

	explicitShared := &UserSubscription{
		Kind:          SubscriptionKindTime,
		Scope:         SubscriptionScopeAll,
		UpgradeGroups: []string{"vip"},
	}
	assert.True(t, subscriptionCoversGroup(explicitShared, "default"),
		"scope=all is not bound by its snapshot")

	groupScoped := &UserSubscription{
		Kind:          SubscriptionKindTime,
		Scope:         SubscriptionScopeGroup,
		UpgradeGroups: []string{"vip"},
	}
	assert.True(t, subscriptionCoversGroup(groupScoped, "vip"))
	assert.False(t, subscriptionCoversGroup(groupScoped, "default"),
		"a group-scoped row only serves its own group")

	assert.False(t, subscriptionCoversGroup(nil, "vip"))
}
