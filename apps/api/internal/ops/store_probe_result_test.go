package ops

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

func withProbeDB(t *testing.T) {
	t.Helper()
	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelProbeResult{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previousDB })
}

func probeResultRow(t *testing.T, channelId int, model string, success bool, errorCode string, createdAt int64) {
	require.NoError(t, dbx.DB.Create(&ChannelProbeResult{
		ChannelId: channelId,
		Model:     model,
		Success:   success,
		LatencyMs: 300,
		ErrorCode: errorCode,
		CreatedAt: createdAt,
	}).Error)
}

func TestLoadProbeStats24h(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	// channel 1 / gpt-4o：12 次探测，8 成功 4 失败，最近一次成功
	for i := 0; i < 12; i++ {
		success := i < 8
		errorCode := ""
		if !success {
			errorCode = "upstream_error"
		}
		probeResultRow(t, 1, "gpt-4o", success, errorCode, now-int64(1600-i*100))
	}
	// channel 1 / gpt-4o-mini：2 次，1 成功 1 失败，最近一次失败
	probeResultRow(t, 1, "gpt-4o-mini", true, "", now-200)
	probeResultRow(t, 1, "gpt-4o-mini", false, "fail-x", now-100)

	// channel 2 / claude：唯一一条已超 24h 窗口，应被剔除
	probeResultRow(t, 2, "claude", true, "", now-90000)

	stats, err := LoadProbeStats24h(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, stats, 2)

	byModel := map[string]ProbeModelStat{}
	for _, s := range stats {
		byModel[s.Model] = s
	}
	gpt4o, ok := byModel["gpt-4o"]
	require.True(t, ok)
	require.Equal(t, int64(8), gpt4o.SuccessCount)
	require.Equal(t, int64(12), gpt4o.TotalCount)
	require.NotNil(t, gpt4o.Availability)
	require.InDelta(t, 66.66666666666667, *gpt4o.Availability, 1e-9)
	require.Equal(t, int64(now-500), gpt4o.LastProbeAt)
	require.Equal(t, "", gpt4o.LastErrorCode)

	mini, ok := byModel["gpt-4o-mini"]
	require.True(t, ok)
	require.Equal(t, int64(1), mini.SuccessCount)
	require.Equal(t, int64(2), mini.TotalCount)
	require.NotNil(t, mini.Availability)
	require.InDelta(t, 50.0, *mini.Availability, 1e-9)
	require.Equal(t, int64(now-100), mini.LastProbeAt)
	require.Equal(t, "fail-x", mini.LastErrorCode)

	// 窗口外与未探测渠道：无数据
	empty, err := LoadProbeStats24h(context.Background(), 2)
	require.NoError(t, err)
	require.Empty(t, empty)
}
