package channel_test

import (
	"testing"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The channel half of the signed-delta accounting contract. The user half lives
// in internal/identity; catalog imports identity, so one test cannot assert
// both sides without an import cycle.
func TestChannelUsageAccountingSupportsSignedDirectAndBatchDeltas(t *testing.T) {
	truncateTables(t)

	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	dbx.DrainBatchQueues()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		dbx.DrainBatchQueues()
	})

	channel := catalog.Channel{
		Id:        10,
		Name:      "usage-adjustment-channel",
		Key:       "sk-test",
		Status:    common.ChannelStatusEnabled,
		UsedQuota: 1000,
	}
	require.NoError(t, dbx.DB.Create(&channel).Error)

	catalog.UpdateChannelUsedQuota(channel.Id, -200)
	catalog.UpdateChannelUsedQuota(channel.Id, 50)

	var gotChannel catalog.Channel
	require.NoError(t, dbx.DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(850), gotChannel.UsedQuota)

	common.BatchUpdateEnabled = true
	catalog.UpdateChannelUsedQuota(channel.Id, 400)
	catalog.UpdateChannelUsedQuota(channel.Id, -100)

	require.NoError(t, dbx.DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(850), gotChannel.UsedQuota, "batch deltas must remain queued until flush")

	dbx.FlushBatchQueues()

	require.NoError(t, dbx.DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(1150), gotChannel.UsedQuota)
}
