package ops

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// UpstreamPriceSnapshot records one upstream pricing field value for one model
// in one fetch batch. The shared fetched_at unix-second key groups the rows of
// a batch so /fluctuation can compare two consecutive fetches.
type UpstreamPriceSnapshot struct {
	ID        int    `json:"id" gorm:"primaryKey"`
	FetchedAt int64  `json:"fetched_at" gorm:"bigint;index"`
	Source    string `json:"source" gorm:"index"`
	ModelName string `json:"model_name" gorm:"index"`
	Field     string `json:"field" gorm:"index"`
	Value     string `json:"value" gorm:"type:text"`
}

// TableName pins the snake_case table name across all supported dialects.
func (UpstreamPriceSnapshot) TableName() string { return "upstream_price_snapshots" }

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &UpstreamPriceSnapshot{}, Name: "UpstreamPriceSnapshot"})
}

// persistUpstreamPriceSnapshots writes one row per (source, model_name, field)
// from successfulChannels under a single fetched_at batch key. Persistence is
// best effort: a failure is only logged and never breaks the fetch response.
func persistUpstreamPriceSnapshots(fetchedAt int64, successfulChannels []struct {
	name string
	data map[string]any
}) {
	rows := make([]UpstreamPriceSnapshot, 0, len(successfulChannels))
	for _, ch := range successfulChannels {
		for field, byModel := range ch.data {
			modelMap, ok := byModel.(map[string]any)
			if !ok {
				continue
			}
			for modelName, value := range modelMap {
				raw, err := common.Marshal(value)
				if err != nil {
					continue
				}
				rows = append(rows, UpstreamPriceSnapshot{
					FetchedAt: fetchedAt,
					Source:    ch.name,
					ModelName: modelName,
					Field:     field,
					Value:     string(raw),
				})
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	// Replace only the sources this batch actually re-fetched. The batch key has
	// second resolution and each /fetch carries its own selection, so deleting
	// the whole second would let one request erase a concurrent request's
	// channels (both write the same fetched_at). Scoping the delete by source
	// keeps "same source, same second replaces" without cross-channel loss.
	sources := make([]string, 0, len(successfulChannels))
	seen := make(map[string]struct{}, len(successfulChannels))
	for _, ch := range successfulChannels {
		if _, ok := seen[ch.name]; ok {
			continue
		}
		seen[ch.name] = struct{}{}
		sources = append(sources, ch.name)
	}
	if err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("fetched_at = ? AND source IN ?", fetchedAt, sources).Delete(&UpstreamPriceSnapshot{}).Error; err != nil {
			return err
		}
		return tx.Create(rows).Error
	}); err != nil {
		common.SysError("failed to persist upstream price snapshots: " + err.Error())
	}
	trimUpstreamPriceSnapshots()
}

// snapshotRetentionBatches bounds the snapshot table: the newest N distinct
// batches are kept, older ones are deleted after each persist. Fluctuation
// reads the two most recent batches and analysis the latest per source, so a
// small window is enough even under the frontend's 5-minute auto refresh.
const snapshotRetentionBatches = 12

// trimUpstreamPriceSnapshots deletes every batch older than the newest
// snapshotRetentionBatches ones. Best effort: failures are only logged.
func trimUpstreamPriceSnapshots() {
	var batches []int64
	if err := dbx.DB.Model(&UpstreamPriceSnapshot{}).
		Distinct().Order("fetched_at DESC").
		Limit(snapshotRetentionBatches).Pluck("fetched_at", &batches).Error; err != nil {
		common.SysError("failed to list snapshot batches for trim: " + err.Error())
		return
	}
	if len(batches) < snapshotRetentionBatches {
		return
	}
	cutoff := batches[len(batches)-1]
	if err := dbx.DB.Where("fetched_at < ?", cutoff).
		Delete(&UpstreamPriceSnapshot{}).Error; err != nil {
		common.SysError("failed to trim upstream price snapshots: " + err.Error())
	}
}
