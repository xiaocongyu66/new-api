package channel

// Bulk channel deletion must take the channel's route units with it. Before this
// was wired, DeleteChannelModelRoutesByChannelIDsWithTx had no production caller:
// the three bulk paths deleted channels, abilities and route health but left the
// route rows behind. Orphaned rows stay `enabled`, so they keep diluting
// share-correction and keep showing up in the admin route-unit view for channels
// that no longer exist.
//
// Each test asserts through the exported bulk entry point rather than the
// tx-internal helper, so removing the delete call from any one of the three paths
// fails here.

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupBulkDeleteRouteTest installs a database holding every table the bulk
// delete paths touch, including the gateway revision singleton that
// MutateGatewayRouting bumps inside the same transaction.
func setupBulkDeleteRouteTest(t *testing.T) {
	t.Helper()

	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Channel{}, &Ability{}, &ChannelModelRoute{}, &ChannelModelHealth{},
		&GatewayConfigRevision{}, &GatewayConfigOutbox{},
	))
	dbx.DB = db
	require.NoError(t, InitializeGatewayConfigRevision())

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

// seedChannelWithRoutes creates a channel and its expanded route rows, returning
// the number of route rows written so a test can assert they existed first.
func seedChannelWithRoutes(t *testing.T, id int, status int) int64 {
	t.Helper()

	require.NoError(t, dbx.DB.Create(&Channel{
		Id:     id,
		Key:    "sk-test",
		Models: "model-a,model-b",
		Group:  "default",
		Status: status,
	}).Error)
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return SyncChannelModelRoutesWithTx(tx, id)
	}))

	count := routeCountFor(t, id)
	require.Positive(t, count, "fixture must start with route rows for channel %d", id)
	return count
}

func routeCountFor(t *testing.T, channelID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, dbx.DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", channelID).Count(&count).Error)
	return count
}

func TestBatchDeleteChannelsRemovesRouteRows(t *testing.T) {
	setupBulkDeleteRouteTest(t)

	seedChannelWithRoutes(t, 7101, common.ChannelStatusEnabled)
	seedChannelWithRoutes(t, 7102, common.ChannelStatusEnabled)
	survivor := seedChannelWithRoutes(t, 7103, common.ChannelStatusEnabled)

	deleted, err := BatchDeleteChannels([]int{7101, 7102})
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)

	assert.Zero(t, routeCountFor(t, 7101), "batch delete must take the route units with the channel")
	assert.Zero(t, routeCountFor(t, 7102))
	assert.Equal(t, survivor, routeCountFor(t, 7103),
		"an untouched channel must keep every route row")
}

func TestDeleteChannelByStatusRemovesRouteRows(t *testing.T) {
	setupBulkDeleteRouteTest(t)

	seedChannelWithRoutes(t, 7201, common.ChannelStatusManuallyDisabled)
	survivor := seedChannelWithRoutes(t, 7202, common.ChannelStatusEnabled)

	deleted, err := DeleteChannelByStatus(common.ChannelStatusManuallyDisabled)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	assert.Zero(t, routeCountFor(t, 7201), "status delete must take the route units with the channel")
	assert.Equal(t, survivor, routeCountFor(t, 7202))
}

func TestDeleteDisabledChannelRemovesRouteRows(t *testing.T) {
	setupBulkDeleteRouteTest(t)

	seedChannelWithRoutes(t, 7301, common.ChannelStatusAutoDisabled)
	seedChannelWithRoutes(t, 7302, common.ChannelStatusManuallyDisabled)
	survivor := seedChannelWithRoutes(t, 7303, common.ChannelStatusEnabled)

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)

	assert.Zero(t, routeCountFor(t, 7301), "disabled-channel delete must take both disabled statuses' route units")
	assert.Zero(t, routeCountFor(t, 7302))
	assert.Equal(t, survivor, routeCountFor(t, 7303))
}
