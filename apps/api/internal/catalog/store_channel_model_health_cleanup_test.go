package channel

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertUnitRowPresent checks that a unit has a persisted row (DB is the
// source of truth for the cleanup path).
func assertUnitRowPresent(t *testing.T, channelID int, keyIndex int, model string) {
	t.Helper()
	var count int64
	require.NoError(t, dbx.DB.Model(&ChannelModelHealth{}).
		Where("channel_id = ? AND key_index = ? AND model = ?", channelID, keyIndex, model).
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "unit (%d,%d,%s) should have a DB row", channelID, keyIndex, model)
}

// assertUnitRowGone checks that a unit has no persisted row.
func assertUnitRowGone(t *testing.T, channelID int, keyIndex int, model string) {
	t.Helper()
	var count int64
	require.NoError(t, dbx.DB.Model(&ChannelModelHealth{}).
		Where("channel_id = ? AND key_index = ? AND model = ?", channelID, keyIndex, model).
		Count(&count).Error)
	assert.Equal(t, int64(0), count, "unit (%d,%d,%s) should have no DB row", channelID, keyIndex, model)
}

// seedCoolingUnit persists one unit row in a live cooling state (EWMA lowered,
// request count established, a future cooldown deadline) so it is a non-default
// row the cleanup must survive or remove.
func seedCoolingUnit(t *testing.T, channelID int, keyIndex int, model string, cooldownUntilMs int64) {
	t.Helper()
	seedUnitHealthRow(t, &ChannelModelHealth{
		ChannelId:          channelID,
		KeyIndex:           keyIndex,
		Model:              model,
		EwmaScore:          0.4,
		RequestCount:       6,
		CooldownUntilMs:    cooldownUntilMs,
		LastCoolingOutcome: int(UnitFatal),
		Version:            3,
	})
}

// ---- A. deleteUnitHealthByChannelIDsWithTx: clears DB rows and cache ----

// TestDeleteUnitHealthByChannelIDSClearsRowsAndCache verifies that deleting
// by channel ID removes both the persisted unit rows and the in-memory cache
// entries for those channels, while leaving sibling channels untouched.
// Fails if: the function only deletes DB rows but forgets the cache (a ghost
// cooling state suppresses a healthy unit), or vice-versa, or if it over-deletes
// into other channels.
func TestDeleteUnitHealthByChannelIDSClearsRowsAndCache(t *testing.T) {
	withUnitHealthDB(t)
	futureMs := time.Now().Add(time.Hour).UnixMilli()
	seedCoolingUnit(t, 8801, 0, "model-alpha", futureMs)
	seedCoolingUnit(t, 8801, 0, "model-beta", futureMs)
	seedCoolingUnit(t, 8802, 0, "model-gamma", futureMs)

	require.NoError(t, deleteUnitHealthByChannelIDsWithTx(dbx.DB, []int{8801}))

	assertUnitRowGone(t, 8801, 0, "model-alpha")
	assertUnitRowGone(t, 8801, 0, "model-beta")
	assertUnitRowPresent(t, 8802, 0, "model-gamma")
	// The mirror must have evicted the doomed channel's keys only.
	assert.False(t, IsUnitSelectable(RouteKey{ChannelId: 8802, KeyIndex: 0, Model: "model-gamma"}, timeNow()))
}

// TestDeleteUnitHealthByChannelIDsNilIDsIsNoOp verifies that a nil/empty ID
// list is a no-op — the guard clause prevents a `WHERE channel_id IN ()` SQL
// error or an accidental full-table cache wipe.
func TestDeleteUnitHealthByChannelIDsNilIDsIsNoOp(t *testing.T) {
	withUnitHealthDB(t)
	seedCoolingUnit(t, 8803, 0, "model-delta", 1_000_000)

	require.NoError(t, deleteUnitHealthByChannelIDsWithTx(dbx.DB, nil))
	require.NoError(t, deleteUnitHealthByChannelIDsWithTx(dbx.DB, []int{}))

	assertUnitRowPresent(t, 8803, 0, "model-delta")
}

// ---- B. deleteUnitHealthNotInModelsWithTx: preserve semantics ----

// TestDeleteUnitHealthNotInModelsPreservesKeptRows is the most critical
// invariant: editing a channel's model list must delete unit rows for removed
// models while preserving the state of every surviving model, in both DB and
// cache. A delete-and-rebuild approach would reset the health state and
// silently re-enable an unhealthy unit.
func TestDeleteUnitHealthNotInModelsPreservesKeptRows(t *testing.T) {
	withUnitHealthDB(t)
	seedCoolingUnit(t, 8804, 0, "kept-a", 1_000_000)
	seedCoolingUnit(t, 8804, 0, "removed-b", 1_000_000)

	require.NoError(t, deleteUnitHealthNotInModelsWithTx(dbx.DB, 8804, []string{"kept-a"}))

	assertUnitRowPresent(t, 8804, 0, "kept-a")
	assertUnitRowGone(t, 8804, 0, "removed-b")

	// An empty keep-list clears the whole channel.
	seedCoolingUnit(t, 8805, 0, "only-c", 1_000_000)
	require.NoError(t, deleteUnitHealthNotInModelsWithTx(dbx.DB, 8805, []string{}))
	assertUnitRowGone(t, 8805, 0, "only-c")
}

// ---- C. Channel.deleteWithTx: end-to-end channel deletion ----

// TestChannelDeleteWithTxCleansUnitHealth verifies the end-to-end path from
// channel deletion down to unit-row cleanup.
func TestChannelDeleteWithTxCleansUnitHealth(t *testing.T) {
	withUnitHealthDB(t)
	channel := seedDeletableChannel(t, 8806, "model-a")
	seedCoolingUnit(t, channel.Id, 0, "model-a", 1_000_000)

	require.NoError(t, channel.deleteWithTx(dbx.DB))

	assertUnitRowGone(t, channel.Id, 0, "model-a")
}

// ---- D. Channel.UpdateAbilities: model-list edit end-to-end ----

// TestUpdateAbilitiesRemovesOrphanedUnitHealth verifies that when a channel's
// model list is narrowed, UpdateAbilities deletes the unit rows for the removed
// model while preserving the state of the kept model.
func TestUpdateAbilitiesRemovesOrphanedUnitHealth(t *testing.T) {
	withUnitHealthDB(t)
	channel := seedDeletableChannel(t, 8808, "model-a,model-b")
	seedCoolingUnit(t, channel.Id, 0, "model-a", 1_000_000)
	seedCoolingUnit(t, channel.Id, 0, "model-b", 1_000_000)

	// Narrow the model list in memory: UpdateAbilities reads channel.Models,
	// not the DB row.
	channel.Models = "model-a"
	require.NoError(t, channel.UpdateAbilities(dbx.DB))

	assertUnitRowGone(t, channel.Id, 0, "model-b")
	assertUnitRowPresent(t, channel.Id, 0, "model-a")
}

// ---- E. Key-range cleanup ----

// TestDeleteUnitHealthOutsideKeyRangeClearsOrphanedKeys verifies that after a
// channel key-list shrink, unit rows for the now-orphaned key indices are
// removed while the surviving key's state is preserved.
func TestDeleteUnitHealthOutsideKeyRangeClearsOrphanedKeys(t *testing.T) {
	withUnitHealthDB(t)
	seedCoolingUnit(t, 8809, 0, "model-d", 1_000_000)
	seedCoolingUnit(t, 8809, 1, "model-d", 1_000_000)
	seedCoolingUnit(t, 8809, 2, "model-d", 1_000_000)

	require.NoError(t, deleteUnitHealthOutsideKeyRangeWithTx(dbx.DB, 8809, 2))

	assertUnitRowPresent(t, 8809, 0, "model-d")
	assertUnitRowPresent(t, 8809, 1, "model-d")
	assertUnitRowGone(t, 8809, 2, "model-d")
}

// seedDeletableChannel creates a channel plus its abilities so deleteWithTx /
// UpdateAbilities have the surrounding rows they operate on.
func seedDeletableChannel(t *testing.T, id int, models string) *Channel {
	t.Helper()
	channel := &Channel{
		Id:     id,
		Type:   1,
		Key:    "sk-seed",
		Status: common.ChannelStatusEnabled,
		Name:   "cleanup-channel-" + strconv.Itoa(id),
		Models: models,
		Group:  "default",
	}
	require.NoError(t, dbx.DB.Create(channel).Error)
	for _, model := range strings.Split(models, ",") {
		if model == "" {
			continue
		}
		require.NoError(t, dbx.DB.Create(&Ability{
			Group:     "default",
			Model:     model,
			ChannelId: id,
			Enabled:   true,
		}).Error)
	}
	return channel
}

// timeNow is a tiny indirection over the package clock so cache assertions
// use the same now as the store's hot path.
func timeNow() time.Time {
	return ChannelHealthNow()
}
