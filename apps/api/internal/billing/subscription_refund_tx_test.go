package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The refund must commit the subscription usage reversal and the pre-consume
// record status in one transaction. The old code nested a second dbx.DB
// transaction inside the outer one, which on SQLite deadlocks behind the
// outer locks (the test fixture pins a single pooled connection) and on
// MySQL/PostgreSQL could commit the refund while the outer status update
// rolled back, letting a retry refund the same request twice.

func seedPreConsumeForRefundTest(t *testing.T, requestId string, amountTotal, amountUsed, preConsumed int64) *UserSubscription {
	t.Helper()
	sub := &UserSubscription{
		UserId:      8100,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		StartTime:   common.GetTimestamp() - 60,
		EndTime:     common.GetTimestamp() + 3600,
		Status:      "active",
		Source:      "order",
	}
	require.NoError(t, dbx.DB.Create(sub).Error)
	record := &SubscriptionPreConsumeRecord{
		RequestId:          requestId,
		UserId:             8100,
		UserSubscriptionId: sub.Id,
		PreConsumed:        preConsumed,
		Status:             "consumed",
	}
	require.NoError(t, dbx.DB.Create(record).Error)
	return sub
}

func TestRefundSubscriptionPreConsumeSingleTransaction(t *testing.T) {
	setupIdentityTestDB(t)
	sub := seedPreConsumeForRefundTest(t, "req-refund-ok", 1000, 600, 300)

	// Before the fix this call could not complete at all against the pinned
	// single-connection SQLite pool: the inner transaction waited on locks
	// held by the outer transaction forever.
	require.NoError(t, RefundSubscriptionPreConsume("req-refund-ok"))

	var reloaded UserSubscription
	require.NoError(t, dbx.DB.First(&reloaded, sub.Id).Error)
	assert.Equal(t, int64(300), reloaded.AmountUsed)

	var record SubscriptionPreConsumeRecord
	require.NoError(t, dbx.DB.Where("request_id = ?", "req-refund-ok").First(&record).Error)
	assert.Equal(t, "refunded", record.Status)

	// Refund is idempotent: a retry must not reverse the usage a second time.
	require.NoError(t, RefundSubscriptionPreConsume("req-refund-ok"))
	require.NoError(t, dbx.DB.First(&reloaded, sub.Id).Error)
	assert.Equal(t, int64(300), reloaded.AmountUsed)
}

// A refund whose usage reversal fails (subscription row missing) must roll the
// record status back with it — single transaction means no half state where
// the record reads "refunded" while the quota was never returned.
func TestRefundSubscriptionPreConsumeRollsBackOnDeltaFailure(t *testing.T) {
	setupIdentityTestDB(t)
	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-refund-missing-sub",
		UserId:             8102,
		UserSubscriptionId: 424242,
		PreConsumed:        100,
		Status:             "consumed",
	}
	require.NoError(t, dbx.DB.Create(record).Error)

	require.Error(t, RefundSubscriptionPreConsume("req-refund-missing-sub"))

	var reloaded SubscriptionPreConsumeRecord
	require.NoError(t, dbx.DB.Where("request_id = ?", "req-refund-missing-sub").First(&reloaded).Error)
	assert.Equal(t, "consumed", reloaded.Status)
}
