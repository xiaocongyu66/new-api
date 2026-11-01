package usage

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatsDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousLogDB := dbx.LogDB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE logs (
		id INTEGER PRIMARY KEY,
		type INTEGER,
		channel_id INTEGER,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		use_time INTEGER,
		quota INTEGER,
		created_at INTEGER
	)`).Error)
	dbx.LogDB = db

	t.Cleanup(func() {
		dbx.LogDB = previousLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedChannelStatsLogs(t *testing.T, db *gorm.DB, rows string, args ...any) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO logs (type, channel_id, prompt_tokens, completion_tokens, use_time, quota, created_at) VALUES `+rows,
		args...,
	).Error, "seed channel stats logs")
}

func TestGetChannelRequestStatsAggregatesPerChannel(t *testing.T) {
	db := setupChannelStatsDB(t)

	now := time.Now().Unix()
	startTime := now - 86400
	endTime := now

	// 渠道 1：2 条成功(2) + 1 条失败(5)，窗口内；渠道 2：仅失败；
	// 渠道 3：无日志；最后一条渠道 1 的写入超出窗口期，应被过滤。
	seedChannelStatsLogs(t, db,
		`(2, 1, 100, 50, 2, 10, ?),
		 (2, 1, 200, 300, 4, 20, ?),
		 (5, 1, 0, 0, 0, 5, ?),
		 (5, 2, 0, 0, 0, 3, ?),
		 (5, 2, 0, 0, 0, 3, ?),
		 (2, 1, 10, 10, 1, 1, ?)`,
		now-100, now-200, now-300, now-400, now-500, now-200000)

	previousHook := OnQuotaToDisplayAmount
	OnQuotaToDisplayAmount = nil
	defer func() { OnQuotaToDisplayAmount = previousHook }()

	stats, err := GetChannelRequestStats([]int{1, 2, 3}, startTime, endTime)
	require.NoError(t, err)

	got1, ok := stats[1]
	require.True(t, ok, "渠道 1 应有聚合结果")
	assert.Equal(t, int64(3), got1.RequestCount, "success+error")
	assert.Equal(t, int64(2), got1.SuccessCount)
	assert.Equal(t, int64(1), got1.ErrorCount)
	assert.Equal(t, int64(650), got1.Tokens, "仅 type=2 的 prompt+completion")
	assert.Equal(t, int64(3000), got1.AvgDurationMs, "avg use_time(秒) * 1000")
	assert.Equal(t, float64(35), got1.QuotaDisplay)
	assert.Equal(t, now-100, got1.LastRequestAt, "MAX(created_at)")

	got2, ok := stats[2]
	require.True(t, ok)
	assert.Equal(t, int64(2), got2.RequestCount)
	assert.Equal(t, int64(0), got2.SuccessCount)
	assert.Equal(t, int64(2), got2.ErrorCount)
	assert.Equal(t, int64(0), got2.Tokens)
	assert.Equal(t, int64(0), got2.AvgDurationMs, "无成功行，时长为 0")
	assert.Equal(t, float64(6), got2.QuotaDisplay)
	assert.Equal(t, now-400, got2.LastRequestAt)

	_, ok = stats[3]
	assert.False(t, ok, "窗口期内无日志的渠道不应出现在结果中")
}

func TestGetChannelRequestStatsEmptyChannelIDs(t *testing.T) {
	setupChannelStatsDB(t)

	stats, err := GetChannelRequestStats(nil, time.Now().Unix()-86400, time.Now().Unix())
	require.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Empty(t, stats, "channel_ids 为空应返回空 map")
}
