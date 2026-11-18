package ops

import (
	"context"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
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
