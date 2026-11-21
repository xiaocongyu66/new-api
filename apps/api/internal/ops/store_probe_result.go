package ops

import (
	"context"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/usage"
)

type ChannelProbeResult struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	ChannelId int    `json:"channel_id" gorm:"index:idx_probe_chan_model,priority:1"`
	Model     string `json:"model" gorm:"index:idx_probe_chan_model,priority:2;type:varchar(255)"`
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	ErrorCode string `json:"error_code" gorm:"type:varchar(64)"`
	CreatedAt int64  `json:"created_at" gorm:"index"` // Unix 秒
}

func (ChannelProbeResult) TableName() string { return "channel_probe_results" }

// ProbeModelStat 聚合单个渠道 24h 内某模型的探测结果。
type ProbeModelStat struct {
	Model         string   `json:"model"`
	SuccessCount  int64    `json:"success_count"`
	TotalCount    int64    `json:"total_count"`
	Availability  *float64 `json:"availability"`  // nil = 无数据
	LastProbeAt   int64    `json:"last_probe_at"` // Unix 秒，0 = 无
	LastErrorCode string   `json:"last_error_code"`
}

const probeResultWindowSeconds = 24 * 3600

// LoadProbeStats24h 聚合单个渠道 24h 内的探测结果（按模型分组）。
func LoadProbeStats24h(ctx context.Context, channelId int) ([]ProbeModelStat, error) {
	_ = ctx
	cutoff := common.GetTimestamp() - probeResultWindowSeconds
	var rows []ChannelProbeResult
	if err := dbx.DB.Where("channel_id = ? AND created_at > ?", channelId, cutoff).
		Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return AggregateProbeModels(rows), nil
}

// LoadAllProbeResults24h 一次查询 24h 内全部探测行（量级受控），供 overview 与阈值判定
// 在 Go 内按 channel_id 分组聚合，避免布尔 SUM 的方言差异。
func LoadAllProbeResults24h(ctx context.Context) ([]ChannelProbeResult, error) {
	_ = ctx
	cutoff := common.GetTimestamp() - probeResultWindowSeconds
	var rows []ChannelProbeResult
	if err := dbx.DB.Where("created_at > ?", cutoff).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

type upstreamHealthRow struct {
	ChannelID          int     `gorm:"column:channel_id"`
	Model              string  `gorm:"column:model"`
	Rate               float64 `gorm:"column:rate"`
	LatencyMs          float64 `gorm:"column:latency_ms"`
	LatencyUpdatedAtMs int64   `gorm:"column:latency_updated_at_ms"`
	ObservedAt         int64   `gorm:"column:observed_at"`
	Weight             float64 `gorm:"column:weight"`
}

// LoadUpstreamHealth returns one observed health estimate per enabled
// channel/model in the recent window. Health rows without a score update are
// excluded so the default 1.0 score is never presented as a measurement.
func LoadUpstreamHealth(ctx context.Context, startTs, endTs int64, groups []string) ([]usage.UpstreamHealthPoint, error) {
	if startTs < endTs-probeResultWindowSeconds {
		startTs = endTs - probeResultWindowSeconds
	}
	if len(groups) == 0 && groups != nil {
		return nil, nil
	}

	abilities := dbx.DB.Model(&channelpkg.Ability{}).
		Select("DISTINCT channel_id, model").
		Where("enabled = ?", true)
	if groups != nil {
		abilities = abilities.Where(dbx.GroupCol()+" IN ?", groups)
	}

	var rows []upstreamHealthRow
	err := dbx.DB.WithContext(ctx).Table("channel_model_health AS health").
		Select("health.channel_id, health.model, health.ewma_score * 100 AS rate, health.latency_ewma_ms AS latency_ms, health.latency_updated_at_ms, health.score_updated_at_ms AS observed_at, routes.static_weight AS weight").
		Joins("JOIN channel_model_routes AS routes ON routes.channel_id = health.channel_id AND routes.key_index = health.key_index AND routes.public_model_alias = health.model AND routes.enabled = ?", true).
		Joins("JOIN channels ON channels.id = health.channel_id AND channels.status = ?", common.ChannelStatusEnabled).
		Joins("JOIN (?) AS abilities ON abilities.channel_id = health.channel_id AND abilities.model = health.model", abilities).
		Where("health.score_updated_at_ms >= ? AND health.score_updated_at_ms <= ?", startTs*1000, endTs*1000).
		Order("health.channel_id, health.model, health.key_index").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	type healthKey struct {
		channelID int
		model     string
	}
	type healthAggregate struct {
		signals      []usage.RateWeightSignal
		weight       float64
		latencySum   float64
		latencyCount int64
		observedAt   int64
	}
	aggregates := make(map[healthKey]healthAggregate, len(rows))
	for _, row := range rows {
		key := healthKey{channelID: row.ChannelID, model: row.Model}
		agg := aggregates[key]
		agg.signals = append(agg.signals, usage.RateWeightSignal{Rate: row.Rate, Weight: row.Weight})
		if row.Weight > 0 {
			agg.weight += row.Weight
		}
		if row.LatencyMs > 0 && row.LatencyUpdatedAtMs >= (endTs-probeResultWindowSeconds)*1000 && row.LatencyUpdatedAtMs <= endTs*1000 {
			agg.latencySum += row.LatencyMs
			agg.latencyCount++
		}
		if row.ObservedAt > agg.observedAt {
			agg.observedAt = row.ObservedAt
		}
		aggregates[key] = agg
	}

	points := make([]usage.UpstreamHealthPoint, 0, len(aggregates))
	for key, agg := range aggregates {
		point := usage.UpstreamHealthPoint{
			Model:      key.model,
			ChannelID:  key.channelID,
			Rate:       usage.WeightedRate(agg.signals),
			ObservedAt: agg.observedAt / 1000,
			Weight:     agg.weight,
		}
		if agg.latencyCount > 0 {
			point.LatencyMs = int64(agg.latencySum / float64(agg.latencyCount))
		}
		points = append(points, point)
	}
	return points, nil
}

// AggregateProbeModels 将探测行按模型聚合：成功/总数、可用率、最近一次探测时间与错误码。
func AggregateProbeModels(rows []ChannelProbeResult) []ProbeModelStat {
	groups := make(map[string]*ProbeModelStat)
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		stat, ok := groups[row.Model]
		if !ok {
			stat = &ProbeModelStat{Model: row.Model}
			groups[row.Model] = stat
			order = append(order, row.Model)
		}
		stat.TotalCount++
		if row.Success {
			stat.SuccessCount++
		}
		if row.CreatedAt > stat.LastProbeAt {
			stat.LastProbeAt = row.CreatedAt
			stat.LastErrorCode = row.ErrorCode
		}
	}
	out := make([]ProbeModelStat, 0, len(order))
	for _, model := range order {
		stat := groups[model]
		if stat.TotalCount > 0 {
			availability := float64(stat.SuccessCount) / float64(stat.TotalCount) * 100
			stat.Availability = &availability
		}
		out = append(out, *stat)
	}
	return out
}
