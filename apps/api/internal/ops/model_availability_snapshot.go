package ops

import (
	"context"
	"math"

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
		Where("created_at >= ?", cutoff).
		Order("model ASC, created_at DESC, id DESC").
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
			ObservedAt: snap.CreatedAt,
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
	var snaps []ModelAvailabilitySnapshot
	if err := dbx.DB.WithContext(ctx).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Order("created_at ASC, id ASC").
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

	// Group snapshots by (model, hour) in ONE pass. Within (model, hour,
	// channel) the LAST row wins (ASC order = newest last).
	type cell struct {
		rate      float64
		latencyMs int64
	}
	type cellKey struct {
		model     string
		channelId int
	}
	type modelHour struct {
		model string
		ts    int64
	}
	cells := make(map[modelHour]map[cellKey]cell)
	order := make([]modelHour, 0)
	seenHour := make(map[modelHour]struct{})
	for _, snap := range snaps {
		mh := modelHour{snap.Model, snap.CreatedAt - snap.CreatedAt%bucketSeconds}
		if _, ok := seenHour[mh]; !ok {
			seenHour[mh] = struct{}{}
			order = append(order, mh)
		}
		if cells[mh] == nil {
			cells[mh] = make(map[cellKey]cell)
		}
		cells[mh][cellKey{snap.Model, snap.ChannelId}] = cell{
			rate: snap.Rate, latencyMs: snap.LatencyMs,
		}
	}

	out := make([]usage.ProbeBucket, 0, len(order))
	for _, mh := range order {
		var wSum, weightedRate, weightedLat, latWeight float64
		var plainRate, plainLat float64
		latCount, plainCount := 0, 0
		for key, c := range cells[mh] {
			w := weights[unitKey{key.channelId, key.model}]
			wSum += w
			weightedRate += c.rate * w
			plainRate += c.rate
			plainCount++
			if c.latencyMs > 0 {
				weightedLat += float64(c.latencyMs) * w
				latWeight += w
				plainLat += float64(c.latencyMs)
				latCount++
			}
		}
		var rate, latency float64
		if wSum > 0 {
			rate = weightedRate / wSum
			if latWeight > 0 {
				latency = weightedLat / latWeight
			}
		} else {
			// No channel has a configured weight: plain mean.
			if plainCount > 0 {
				rate = plainRate / float64(plainCount)
			}
			if latCount > 0 {
				latency = plainLat / float64(latCount)
			}
		}
		// Encode the weighted ratio into SuccessCount/TotalCount so the reader
		// gets a plain ratio; latency is carried directly (no overflow).
		const scale = 1_000_000
		out = append(out, usage.ProbeBucket{
			Model:        mh.model,
			BucketTs:     mh.ts,
			TotalCount:   scale,
			SuccessCount: int64(math.Round(rate / 100 * scale)),
			AvgLatencyMs: int64(math.Round(latency)),
		})
	}
	return out, nil
}
