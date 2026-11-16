package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The credit must reach the wallet and leave a top-up row the user owns, and a
// retry carrying the same request id must not pay out twice.
func TestApplyQuotaCompensationCreditsOncePerRequestId(t *testing.T) {
	user := &identity.User{Username: "compensate-quota-user"}
	require.NoError(t, dbx.DB.Create(user).Error)

	result, err := ApplyQuotaCompensation(user.Id, 205513, "订阅分组闸门补偿", "comp-test-1")
	require.NoError(t, err)
	assert.True(t, result.Applied)

	var quota int
	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", user.Id).
		Select("quota").Find(&quota).Error)
	assert.Equal(t, 205513, quota)

	var logs []usage.Log
	require.NoError(t, dbx.LogDB.Where("user_id = ? AND type = ?", user.Id, usage.LogTypeTopup).
		Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "comp-test-1", logs[0].RequestId)
	assert.Contains(t, logs[0].Content, "订阅分组闸门补偿")

	again, err := ApplyQuotaCompensation(user.Id, 205513, "订阅分组闸门补偿", "comp-test-1")
	require.NoError(t, err)
	assert.False(t, again.Applied)

	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", user.Id).
		Select("quota").Find(&quota).Error)
	assert.Equal(t, 205513, quota, "a repeated request id must not credit again")
}

func TestApplyQuotaCompensationRejectsInvalidInput(t *testing.T) {
	_, err := ApplyQuotaCompensation(0, 100, "x", "")
	assert.Error(t, err)
	_, err = ApplyQuotaCompensation(1, 0, "x", "")
	assert.Error(t, err)
	_, err = ApplyQuotaCompensation(1, 100, "  ", "")
	assert.Error(t, err)
}

// A mistyped id must not leave a credit row behind for a user that does not
// exist: the wallet update would silently affect zero rows.
func TestApplyQuotaCompensationRejectsUnknownUser(t *testing.T) {
	_, err := ApplyQuotaCompensation(999999, 100, "x", "comp-unknown-user")
	assert.Error(t, err)

	var count int64
	require.NoError(t, dbx.LogDB.Model(&usage.Log{}).
		Where("request_id = ?", "comp-unknown-user").Count(&count).Error)
	assert.Zero(t, count)
}
