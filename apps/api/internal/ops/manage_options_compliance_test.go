package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInviteRewardOptionKeysCoverDisplayVariants is the compliance-gate
// regression: the settings page submits QuotaForInviter_display (not the base
// key), and the _display -> base key normalization runs LATER in UpdateOption.
// The gate has to match the _display spelling too, or an admin could bypass
// the payment-compliance requirement by submitting the display key.
func TestInviteRewardOptionKeysCoverDisplayVariants(t *testing.T) {
	for _, key := range []string{
		"QuotaForInviter",
		"QuotaForInvitee",
		"QuotaForInviter_display",
		"QuotaForInvitee_display",
	} {
		assert.True(t, isInviteRewardOptionKey(key),
			"compliance gate must match the settings-page key %q", key)
	}

	for _, key := range []string{
		"QuotaForNewUser",
		"QuotaForNewUser_display",
		"PreConsumedQuota",
		"payment_setting.compliance_confirmed",
		" unrelated_key",
	} {
		assert.False(t, isInviteRewardOptionKey(key),
			"compliance gate must not match unrelated key %q", key)
	}
}
