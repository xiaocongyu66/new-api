package ops

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// withSnapshotDB gives each test a scratch database holding the snapshot
// table, mirroring the withProbeDB pattern used by the probe tests.
func withSnapshotDB(t *testing.T) {
	t.Helper()
	previous := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UpstreamPriceSnapshot{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })
}

func snapRow(fetchedAt int64, source, model, field string, value string) UpstreamPriceSnapshot {
	return UpstreamPriceSnapshot{
		FetchedAt: fetchedAt,
		Source:    source,
		ModelName: model,
		Field:     field,
		Value:     value,
	}
}

// TestPriceFluctuationIgnoresSourcesMissingFromNewerBatch pins the scope
// intersection: /fetch persists only the channels selected for that fetch, so
// a source absent from the newer batch was not fetched again — it must not be
// reported as a price removal.
func TestPriceFluctuationIgnoresSourcesMissingFromNewerBatch(t *testing.T) {
	withSnapshotDB(t)

	require.NoError(t, dbx.DB.Create([]UpstreamPriceSnapshot{
		// Older batch: two sources.
		snapRow(100, "channel-a", "m1", "model_ratio", "1.0"),
		snapRow(100, "channel-b", "m1", "model_ratio", "2.0"),
		// Newer batch: only channel-a was fetched again; its price moved 50%.
		snapRow(200, "channel-a", "m1", "model_ratio", "1.5"),
	}).Error)

	changes := computeFluctuation(10.0)
	for _, ch := range changes {
		assert.NotEqual(t, "channel-b", ch.Source,
			"source absent from the newer batch must not appear as removed")
	}
	require.Len(t, changes, 1)
	assert.Equal(t, "channel-a", changes[0].Source)
	require.NotNil(t, changes[0].ChangePct)
	assert.InDelta(t, 50.0, *changes[0].ChangePct, 1e-9)
}

// TestPersistReplacesSameSecondBatch pins the same-second batch semantics: a
// second fetch in the same second replaces the earlier one instead of merging
// two different channel scopes into one batch.
func TestPersistReplacesSameSecondBatch(t *testing.T) {
	withSnapshotDB(t)

	persist := func(name string) {
		persistUpstreamPriceSnapshots(500, []struct {
			name string
			data map[string]any
		}{{name: name, data: map[string]any{
			"model_ratio": map[string]any{"m1": 1.0},
		}}})
	}
	persist("first")
	persist("second")

	var rows []UpstreamPriceSnapshot
	require.NoError(t, dbx.DB.Where("fetched_at = ?", 500).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "second", rows[0].Source)
}

// TestTrimUpstreamPriceSnapshotsKeepsNewestBatches pins the retention bound:
// after persisting more than snapshotRetentionBatches distinct batches, only
// the newest snapshotRetentionBatches survive.
func TestTrimUpstreamPriceSnapshotsKeepsNewestBatches(t *testing.T) {
	withSnapshotDB(t)

	for i := int64(1); i <= snapshotRetentionBatches+3; i++ {
		row := snapRow(i, "channel-a", "m1", "model_ratio", "1")
		require.NoError(t, dbx.DB.Create(&row).Error)
	}
	persistUpstreamPriceSnapshots(snapshotRetentionBatches+4, []struct {
		name string
		data map[string]any
	}{{name: "channel-a", data: map[string]any{
		"model_ratio": map[string]any{"m1": 1.0},
	}}})

	var batches []int64
	require.NoError(t, dbx.DB.Model(&UpstreamPriceSnapshot{}).
		Distinct().Order("fetched_at").Pluck("fetched_at", &batches).Error)
	require.Len(t, batches, snapshotRetentionBatches)
	// Pre-existing batches 1..15 plus the persisted 16 -> 16 total; keeping the
	// newest 12 leaves 5..16, so the oldest survivor is 5.
	assert.Equal(t, int64(5), batches[0], "oldest surviving batch is the first not trimmed")
}
