package ops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

func withProbeDB(t *testing.T) {
	t.Helper()
	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channelpkg.Ability{}, &channelpkg.Channel{}, &ChannelProbeResult{}))
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
	require.Equal(t, "upstream_error", gpt4o.LastErrorCode)

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

// withHealthDB extends the probe fixture with the persisted route health and
// route tables the upstream loader joins against.
func withHealthDB(t *testing.T) {
	t.Helper()
	withProbeDB(t)
	require.NoError(t, dbx.DB.AutoMigrate(
		&channelpkg.ChannelModelHealth{},
		&channelpkg.ChannelModelRoute{},
	))
}

func TestLoadUpstreamHealthObservedRoutableOnly(t *testing.T) {
	withHealthDB(t)
	now := common.GetTimestamp()
	observedMs := int64(now) * 1000

	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id: 20, Type: 1, Key: "sk-health", Name: "health",
		Models: "routable", Status: common.ChannelStatusEnabled, CreatedTime: now,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.Ability{
		Group: "default", Model: "routable", ChannelId: 20, Enabled: true,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelRoute{
		Id: 1, PublicModelAlias: "routable", ChannelId: 20, KeyIndex: 0,
		UpstreamModel: "routable", StaticWeight: 30, Enabled: true,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelHealth{
		ChannelId: 20, KeyIndex: 0, Model: "routable", Version: 1,
		EwmaScore: 0.6, LatencyEwmaMs: 320, LatencyUpdatedAtMs: observedMs,
		ScoreUpdatedAtMs: observedMs,
	}).Error)

	// A never-observed row and a disabled route must not be reported.
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelHealth{
		ChannelId: 20, KeyIndex: 0, Model: "unobserved", Version: 1,
		EwmaScore: 1.0,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelRoute{
		Id: 2, PublicModelAlias: "disabled-route", ChannelId: 20, KeyIndex: 0,
		UpstreamModel: "disabled-route", Enabled: false,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelHealth{
		ChannelId: 20, KeyIndex: 0, Model: "disabled-route", Version: 1,
		EwmaScore: 0.1, ScoreUpdatedAtMs: observedMs,
	}).Error)

	points, err := LoadUpstreamHealth(context.Background(), now-24*3600, now, []string{"default"})
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.Equal(t, "routable", points[0].Model)
	assert.Equal(t, 20, points[0].ChannelID)
	assert.InDelta(t, 60, points[0].Rate, 0.001)
	assert.Equal(t, int64(320), points[0].LatencyMs)
	assert.Equal(t, now, points[0].ObservedAt)
	assert.Equal(t, float64(30), points[0].Weight)
}
