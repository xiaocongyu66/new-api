package ops

import (
	"context"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/usage"
)

// ModelAvailabilitySnapshot is one durable availability observation for a
// (channel, model) unit.
//
// Availability used to live only in transient aggregates, so a redeploy or a
// restart could blank the model square until new traffic arrived. Persisting
// each run makes the picture survive restarts: the newest snapshot per unit is
// still readable right after a deploy.
//
// Retention is deliberately short and bounded:
//   - rows older than modelAvailabilityTTL are deleted, so a channel that stops
//     serving disappears within a day instead of lingering forever;
//   - at most modelAvailabilityPerModel rows are kept per model, which caps the
//     table no matter how often probing runs.
type ModelAvailabilitySnapshot struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	Model     string `json:"model" gorm:"size:255;index:idx_avail_model_created,priority:1"`
	ChannelId int    `json:"channel_id" gorm:"index"`
	Success   bool   `json:"success"`
	// Rate is 0-100. Own-traffic snapshots carry 100/0; units whose channel
	// has traffic but the model itself does not carry the channel aggregate.
	Rate      float64 `json:"rate"`
	LatencyMs int64   `json:"latency_ms"`
	TtftMs    *int64  `json:"ttft_ms"`
	Source    string  `json:"source" gorm:"size:32"`
	CreatedAt int64   `json:"created_at" gorm:"index:idx_avail_model_created,priority:2;index"`
}

func (ModelAvailabilitySnapshot) TableName() string { return "model_availability_snapshots" }

const (
	// modelAvailabilityTTL bounds how old a snapshot may be. A unit that stops
	// being observed ages out within a day.
	modelAvailabilityTTL = 24 * 3600
	// modelAvailabilityPerModel bounds the rows kept per model.
	modelAvailabilityPerModel = 24
)

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &ModelAvailabilitySnapshot{}, Name: "ModelAvailabilitySnapshot"})
}

// PersistModelAvailability appends one snapshot per unit and then applies
// retention. Best effort: a failure is logged and never breaks the probe run.
func PersistModelAvailability(snapshots []ModelAvailabilitySnapshot) {
	if len(snapshots) > 0 {
		if err := dbx.DB.CreateInBatches(snapshots, 200).Error; err != nil {
			common.SysError("availability snapshot persist failed: " + err.Error())
		}
	}
	// Trim even when the run produced nothing: a quiet window must still age
	// out expired rows, or a channel that stopped serving lingers forever.
	trimModelAvailability()
}

// unitKey identifies one (channel, model) availability unit.
type unitKey struct {
	ChannelId int
	Model     string
}

// trimModelAvailability deletes expired rows and then keeps only the newest
// modelAvailabilityPerModel rows per model.
func trimModelAvailability() {
	now := common.GetTimestamp()
	if err := dbx.DB.Where("created_at < ?", now-modelAvailabilityTTL).
		Delete(&ModelAvailabilitySnapshot{}).Error; err != nil {
		common.SysError("availability snapshot expiry failed: " + err.Error())
	}

	// Bounded per model. The ordered scan is cheap: the table is capped at
	// 24 rows per model by this very function.
	type snapshotRow struct {
		Id        int64
		Model     string
		ChannelId int
	}
	var rows []snapshotRow
	if err := dbx.DB.Model(&ModelAvailabilitySnapshot{}).
		Select("id, model, channel_id").
		Order("model ASC, channel_id ASC, created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		common.SysError("availability snapshot trim scan failed: " + err.Error())
		return
	}
	// The cap is per (channel, model) unit: a model served by N channels gets
	// N rows per run, so counting per model alone would evict whole channels
	// every run.
	seen := make(map[unitKey]int, len(rows))
	drop := make([]int64, 0)
	for _, row := range rows {
		key := unitKey{ChannelId: row.ChannelId, Model: row.Model}
		seen[key]++
		if seen[key] > modelAvailabilityPerModel {
			drop = append(drop, row.Id)
		}
	}
	if len(drop) == 0 {
		return
	}
	if err := dbx.DB.Where("id IN ?", drop).Delete(&ModelAvailabilitySnapshot{}).Error; err != nil {
		common.SysError("availability snapshot trim failed: " + err.Error())
	}
}

// LoadAvailabilitySnapshotsAsHealth turns the newest snapshot per
// (channel, model) into upstream-health points. Every unit on a serving
// channel is returned — including models that never saw traffic, which
// carry their channel's aggregate rate — so the model square stays fully
// charted. Units on channels without static weights are averaged plainly
// by WeightedRate.
func LoadAvailabilitySnapshotsAsHealth(ctx context.Context) ([]usage.UpstreamHealthPoint, error) {
	cutoff := common.GetTimestamp() - modelAvailabilityTTL
	var snaps []ModelAvailabilitySnapshot
	if err := dbx.DB.WithContext(ctx).
		Where("created_at >= ?", cutoff).
		Order("model ASC, created_at DESC, id DESC").
		Find(&snaps).Error; err != nil {
		return nil, err
	}

	seen := make(map[unitKey]struct{}, len(snaps))
	points := make([]usage.UpstreamHealthPoint, 0, len(snaps))
	for _, snap := range snaps {
		key := unitKey{snap.ChannelId, snap.Model}
		if _, ok := seen[key]; ok {
			continue // snapshots arrive newest-first; the first one wins
		}
		seen[key] = struct{}{}
		points = append(points, usage.UpstreamHealthPoint{
			Model:      snap.Model,
			ChannelID:  snap.ChannelId,
			Rate:       snap.Rate,
			LatencyMs:  snap.LatencyMs,
			ObservedAt: snap.CreatedAt,
		})
	}
	return points, nil
}

// LoadAvailabilityProbeBuckets buckets the availability snapshots hourly per
// model, for the model square's timeline: hours where the probe ran show the
// real outcomes it observed (success ratio, latency from outcomes that carry
// one), instead of a flat inherited rate.
func LoadAvailabilityProbeBuckets(ctx context.Context, startTs, endTs, bucketSeconds int64) ([]usage.ProbeBucket, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	var snaps []ModelAvailabilitySnapshot
	if err := dbx.DB.WithContext(ctx).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Order("created_at ASC, id ASC").
		Find(&snaps).Error; err != nil {
		return nil, err
	}

	type bucketKey struct {
		model string
		ts    int64
	}
	buckets := map[bucketKey]*usage.ProbeBucket{}
	order := make([]bucketKey, 0, len(snaps))
	for _, snap := range snaps {
		key := bucketKey{model: snap.Model, ts: snap.CreatedAt - snap.CreatedAt%bucketSeconds}
		bucket, ok := buckets[key]
		if !ok {
			bucket = &usage.ProbeBucket{Model: snap.Model, BucketTs: key.ts}
			buckets[key] = bucket
			order = append(order, key)
		}
		bucket.TotalCount++
		if snap.LatencyMs > 0 {
			bucket.TotalLatencyMs += snap.LatencyMs
		}
		if snap.Success {
			bucket.SuccessCount++
		}
	}
	out := make([]usage.ProbeBucket, 0, len(order))
	for _, key := range order {
		out = append(out, *buckets[key])
	}
	return out, nil
}
