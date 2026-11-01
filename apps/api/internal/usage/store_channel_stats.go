package usage

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// ChannelRequestStat 近 N 小时内单渠道的请求聚合，前端按 channel_id 字符串
// 为 key 读取；该渠道在窗口期内无日志时，map 中不含该键。
type ChannelRequestStat struct {
	RequestCount  int64   `json:"request_count"`
	SuccessCount  int64   `json:"success_count"`
	ErrorCount    int64   `json:"error_count"`
	Tokens        int64   `json:"tokens"`
	AvgDurationMs int64   `json:"avg_duration_ms"`
	QuotaDisplay  float64 `json:"quota_display"`
	LastRequestAt int64   `json:"last_request_at"`
}

type channelRequestStatRow struct {
	ChannelId     int64   `gorm:"column:channel_id"`
	SuccessCount  int64   `gorm:"column:success_count"`
	ErrorCount    int64   `gorm:"column:error_count"`
	Tokens        int64   `gorm:"column:tokens"`
	AvgDurationMs float64 `gorm:"column:avg_duration_ms"`
	Quota         int64   `gorm:"column:quota"`
	LastRequestAt int64   `gorm:"column:last_request_at"`
}

// GetChannelRequestStats 聚合 logs 表中 type=2(请求成功)/5(请求失败) 的行，
// 限制 created_at 在 [startTime, endTime] 之间且 channel_id 在给定集合内，
// 按 channel_id 分组。SQL 仅用 CASE/COALESCE/MAX/AVG，对 SQLite/MySQL/PG
// （以及 ClickHouse）均通用，故不做方言分支。
func GetChannelRequestStats(channelIDs []int, startTime int64, endTime int64) (map[int]ChannelRequestStat, error) {
	if len(channelIDs) == 0 {
		return map[int]ChannelRequestStat{}, nil
	}

	tx := dbx.LogDB.Table("logs").
		Select(`channel_id,
			SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS success_count,
			SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS error_count,
			COALESCE(SUM(CASE WHEN type = ? THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS tokens,
			COALESCE(AVG(CASE WHEN type = ? THEN use_time END) * 1000, 0) AS avg_duration_ms,
			COALESCE(SUM(quota), 0) AS quota,
			MAX(created_at) AS last_request_at`,
			LogTypeConsume, LogTypeError, LogTypeConsume, LogTypeConsume).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Where("type IN (?, ?)", LogTypeConsume, LogTypeError).
		Where("channel_id IN ?", channelIDs).
		Group("channel_id")

	var rows []channelRequestStatRow
	if err := tx.Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[int]ChannelRequestStat, len(rows))
	for _, row := range rows {
		result[int(row.ChannelId)] = ChannelRequestStat{
			RequestCount:  row.SuccessCount + row.ErrorCount,
			SuccessCount:  row.SuccessCount,
			ErrorCount:    row.ErrorCount,
			Tokens:        row.Tokens,
			AvgDurationMs: int64(row.AvgDurationMs),
			QuotaDisplay:  quotaToDisplayAmount64(row.Quota),
			LastRequestAt: row.LastRequestAt,
		}
	}
	return result, nil
}
