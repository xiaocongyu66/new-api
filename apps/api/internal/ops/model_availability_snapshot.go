package ops

import (
	"context"
	"math"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/usage"
	"gorm.io/gorm/clause"
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
	Model     string `json:"model" gorm:"size:255;uniqueIndex:idx_avail_unit,priority:1"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex:idx_avail_unit,priority:2;index"`
	// BucketTs is the upstream's hourly bucket this observation describes.
	// It is the identity of a row together with (model, channel_id): a later
	// probe run refreshes the same hour instead of appending a duplicate.
	BucketTs int64 `json:"bucket_ts" gorm:"uniqueIndex:idx_avail_unit,priority:3;index"`
	// Rate is 0-100, taken verbatim from the upstream's own statistics.
	Rate      float64 `json:"rate"`
	LatencyMs int64   `json:"latency_ms"`
	TtftMs    *int64  `json:"ttft_ms"`
	Source    string  `json:"source" gorm:"size:32"`
	CreatedAt int64   `json:"created_at" gorm:"index"`
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

// PersistModelAvailability upserts one row per (model, channel, hour) and then
// applies retention. Re-probing the same hour refreshes its values instead of
// appending a duplicate, so the table holds one row per hour, not one per run.
// Best effort: a failure is logged and never breaks the probe run.
func PersistModelAvailability(snapshots []ModelAvailabilitySnapshot) {
	if len(snapshots) > 0 {
		if err := dbx.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "model"}, {Name: "channel_id"}, {Name: "bucket_ts"}},
			DoUpdates: clause.AssignmentColumns([]string{"rate", "latency_ms", "ttft_ms", "source", "created_at"}),
		}).CreateInBatches(snapshots, 200).Error; err != nil {
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
	if err := dbx.DB.Where("bucket_ts < ?", now-modelAvailabilityTTL).
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
		Order("model ASC, channel_id ASC, bucket_ts DESC, id DESC").
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

// staticWeightsByUnit sums static_weight across key_index rows for each
// (channel, public_model_alias), matching LoadUpstreamHealth. A configured
// zero-weight route keeps weight 1 (routingBaseWeight(0)==1), so it is not
// silently dropped from a weighted mean; only a missing row gets 0, which
// makes WeightedRate fall back to a plain mean.
func staticWeightsByUnit(ctx context.Context) (map[unitKey]float64, error) {
	var rows []struct {
		ChannelId   int
		PublicModel string
		SumWeight   float64
		RowCount    int64
	}
	if err := dbx.DB.WithContext(ctx).Table("channel_model_routes AS r").
		Select("r.channel_id, r.public_model_alias AS public_model, SUM(r.static_weight) AS sum_weight, COUNT(*) AS row_count").
		Where("r.enabled = ?", true).
		Group("r.channel_id, r.public_model_alias").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[unitKey]float64, len(rows))
	for _, r := range rows {
		w := r.SumWeight
		if r.RowCount > 0 && w == 0 {
			w = 1 // an explicitly configured zero-weight route is still selectable
		}
		out[unitKey{r.ChannelId, r.PublicModel}] = w
	}
	return out, nil
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
		Where("bucket_ts >= ?", cutoff).
		Order("model ASC, bucket_ts DESC, id DESC").
		Find(&snaps).Error; err != nil {
		return nil, err
	}

	weights, err := staticWeightsByUnit(ctx)
	if err != nil {
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
			ObservedAt: snap.BucketTs,
			Weight:     weights[key],
		})
	}
	return points, nil
}

// LoadAvailabilityProbeBuckets buckets the availability snapshots hourly per
// model, for the model square's timeline: hours where the probe ran show the
// real outcomes it observed (success ratio, latency from outcomes that carry
// one), instead of a flat inherited rate.
// LoadAvailabilityProbeBuckets buckets the availability snapshots into one
// hourly point per model. Snapshots from several channels of the same model in
// one hour are combined by STATIC ROUTE WEIGHT (Σrate×weight/Σweight), the
// same prior the router uses — not a plain mean across channels. The result is
// encoded into SuccessCount/TotalCount so the model square can read a plain
// ratio while the weighting has already been applied.
func LoadAvailabilityProbeBuckets(ctx context.Context, startTs, endTs, bucketSeconds int64) ([]usage.ProbeBucket, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	cutoff := startTs - startTs%bucketSeconds
	var snaps []ModelAvailabilitySnapshot
	if err := dbx.DB.WithContext(ctx).
		Where("bucket_ts >= ? AND bucket_ts <= ?", cutoff, endTs).
		Order("model ASC, channel_id ASC, bucket_ts ASC").
		Find(&snaps).Error; err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, nil
	}

	weights, err := staticWeightsByUnit(ctx)
	if err != nil {
		return nil, err
	}

	// 同一模型同一小时、来自多个渠道的上游数据按 static weight 合成一个点。
	type key struct {
		model string
		ts    int64
	}
	groups := make(map[key][]ModelAvailabilitySnapshot, len(snaps))
	order := make([]key, 0, len(snaps))
	for _, snap := range snaps {
		k := key{snap.Model, snap.BucketTs}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], snap)
	}

	out := make([]usage.ProbeBucket, 0, len(order))
	for _, k := range order {
		rows := groups[k]
		var wSum, weightedRate, weightedLat, latWeight, weightedTtft, ttftWeight float64
		var plainRate, plainLat, plainTtft float64
		latCount, ttftCount := 0, 0
		for _, row := range rows {
			w := weights[unitKey{row.ChannelId, row.Model}]
			wSum += w
			weightedRate += row.Rate * w
			plainRate += row.Rate
			if row.LatencyMs > 0 {
				weightedLat += float64(row.LatencyMs) * w
				latWeight += w
				plainLat += float64(row.LatencyMs)
				latCount++
			}
			if row.TtftMs != nil && *row.TtftMs > 0 {
				weightedTtft += float64(*row.TtftMs) * w
				ttftWeight += w
				plainTtft += float64(*row.TtftMs)
				ttftCount++
			}
		}
		var rate, latency, ttft float64
		if wSum > 0 {
			rate = weightedRate / wSum
			if latWeight > 0 {
				latency = weightedLat / latWeight
			}
			if ttftWeight > 0 {
				ttft = weightedTtft / ttftWeight
			}
		} else {
			if n := len(rows); n > 0 {
				rate = plainRate / float64(n)
			}
			if latCount > 0 {
				latency = plainLat / float64(latCount)
			}
			if ttftCount > 0 {
				ttft = plainTtft / float64(ttftCount)
			}
		}
		const scale = 1_000_000
		bucket := usage.ProbeBucket{
			Model:        k.model,
			BucketTs:     k.ts,
			TotalCount:   scale,
			SuccessCount: int64(math.Round(rate / 100 * scale)),
			AvgLatencyMs: int64(math.Round(latency)),
		}
		if ttft > 0 {
			bucket.AvgTtftMs = int64(math.Round(ttft))
		}
		out = append(out, bucket)
	}
	return out, nil
}
