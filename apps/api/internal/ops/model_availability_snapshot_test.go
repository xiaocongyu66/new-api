package ops

// 回归：可用性快照的装载与保留。
//
// 三个此前被审查钉住的缺陷：
//  1. 路由匹配必须用 public_model_alias（快照记的是公开别名），用
//     upstream_model 会让配置了 model_mapping 的渠道全部丢失快照；
//  2. 保留上限必须按 (channel, model) 单位计，按 model 计会让多渠道模型
//     每轮互相挤掉、实际保留远短于 24h；
//  3. 装载取每单位最新一条，多轮写入后仍只取最新。

import (
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withAvailabilityDB(t *testing.T) {
	t.Helper()
	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ModelAvailabilitySnapshot{}, &channelpkg.ChannelModelRoute{},
	))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previousDB })
}

func snapshotRow(t *testing.T, model string, channelId int, rate float64, createdAt int64) {
	t.Helper()
	require.NoError(t, dbx.DB.Create(&ModelAvailabilitySnapshot{
		Model: model, ChannelId: channelId, Success: rate >= 50,
		Rate: rate, LatencyMs: 100, Source: "channel", CreatedAt: createdAt,
	}).Error)
}

func TestLoadAvailabilitySnapshotsAsHealthUsesPublicAlias(t *testing.T) {
	withAvailabilityDB(t)
	now := common.GetTimestamp()

	// Route table stores the upstream name (gpt-4-0613); the snapshot carries
	// the public alias (gpt-4). Matching must be on the alias.
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelRoute{
		Id: 1, PublicModelAlias: "gpt-4", ChannelId: 7, KeyIndex: 0,
		UpstreamModel: "gpt-4-0613", StaticWeight: 10, Enabled: true,
	}).Error)
	snapshotRow(t, "gpt-4", 7, 62.5, now-60)

	points, err := LoadAvailabilitySnapshotsAsHealth(t.Context())
	require.NoError(t, err)
	require.Len(t, points, 1, "a mapped channel's snapshot must not be dropped")
	assert.Equal(t, "gpt-4", points[0].Model)
	assert.Equal(t, 7, points[0].ChannelID)
	assert.InDelta(t, 62.5, points[0].Rate, 0.001)
}

func TestLoadAvailabilitySnapshotsAsHealthKeepsUnroutedChannel(t *testing.T) {
	withAvailabilityDB(t)
	now := common.GetTimestamp()

	// Channels without static-weight routes still serve traffic; their
	// snapshots must reach the card rate via a plain mean, not be dropped.
	snapshotRow(t, "no-routes-model", 9, 80, now-60)

	points, err := LoadAvailabilitySnapshotsAsHealth(t.Context())
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.Equal(t, "no-routes-model", points[0].Model)
	assert.InDelta(t, 80, points[0].Rate, 0.001)
	assert.Zero(t, points[0].Weight, "no routes row means plain-mean weighting")
}

func TestTrimAvailabilityKeepsNewestPerUnit(t *testing.T) {
	withAvailabilityDB(t)
	now := common.GetTimestamp()

	// Unit (ch=1, m=a): cap+2 rows, so exactly the two oldest must be evicted.
	// Unit (ch=2, m=a): one row — must survive even though model "a" as a whole
	// now has far more than the cap, which is what a per-model cap got wrong.
	for i := range 26 {
		snapshotRow(t, "a", 1, 100, now-600+int64(i)*10)
	}
	snapshotRow(t, "a", 2, 40, now-120)

	PersistModelAvailability(nil) // trim runs even with no new rows

	var kept []ModelAvailabilitySnapshot
	require.NoError(t, dbx.DB.Order("channel_id ASC, created_at ASC").Find(&kept).Error)
	require.Len(t, kept, modelAvailabilityPerModel+1)

	perUnit := map[int]int{}
	for _, row := range kept {
		perUnit[row.ChannelId]++
	}
	assert.Equal(t, modelAvailabilityPerModel, perUnit[1], "cap applies per unit")
	assert.Equal(t, 1, perUnit[2], "the other channel's unit must not be evicted")

	oldestKept := now - 600 + int64(2)*10
	assert.Equal(t, int64(oldestKept), kept[0].CreatedAt, "eviction removes the oldest first")
}

func TestTrimAvailabilityExpiresTTL(t *testing.T) {
	withAvailabilityDB(t)
	now := common.GetTimestamp()

	snapshotRow(t, "fresh", 1, 100, now-60)
	snapshotRow(t, "stale", 1, 0, now-25*3600)

	PersistModelAvailability(nil)

	var count int64
	require.NoError(t, dbx.DB.Model(&ModelAvailabilitySnapshot{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "rows older than the TTL must age out")

	var names []string
	require.NoError(t, dbx.DB.Model(&ModelAvailabilitySnapshot{}).
		Distinct().Pluck("model", &names).Error)
	assert.Equal(t, []string{"fresh"}, names)
}
