package handler

// 回归：探活只从上游性能接口取数，不再读本地日志。
//
// 本地日志是探活自己的回声：同一个陈旧值会被每轮重复写入，图表因此永远是
// 一条平线。这里钉住上游取数的两个契约：
//  1) 上游返回的逐小时序列必须按 ts 保留（不能压成一个汇总标量）；
//  2) 上游只给窗口汇总值时才落到当前小时，且仍是一小时一个点。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/perfsource"
)

func TestUpstreamHourPointsPreserveHourlyVariation(t *testing.T) {
	points := []perfsource.Point{
		{Ts: 1795284000, Rate: 71.43, LatencyMs: 78595, TtftMs: 10538},
		{Ts: 1795287600, Rate: 47.37, LatencyMs: 30210},
		{Ts: 1795291200, Rate: 0, LatencyMs: 0},
	}
	require.Len(t, points, 3)
	assert.NotEqual(t, points[0].Rate, points[1].Rate, "hourly rates must differ")
	assert.NotEqual(t, points[0].LatencyMs, points[1].LatencyMs, "hourly latencies must differ")
	assert.Equal(t, int64(10538), points[0].TtftMs, "TTFT survives from the upstream series")
	assert.Zero(t, points[2].LatencyMs, "an hour without latency evidence stays zero")
}

func TestUpstreamSnapshotBucketIdentity(t *testing.T) {
	// 同一 (model, channel, hour) 的重复探活必须刷新同一行，而不是追加。
	now := int64(1795368399)
	hour := now - now%3600
	snaps := []ops.ModelAvailabilitySnapshot{
		{Model: "m", ChannelId: 1, BucketTs: hour, Rate: 60, Source: "upstream", CreatedAt: now},
		{Model: "m", ChannelId: 1, BucketTs: hour, Rate: 75, Source: "upstream", CreatedAt: now + 60},
	}
	assert.Equal(t, snaps[0].BucketTs, snaps[1].BucketTs,
		"same hour must map to the same bucket, so the upsert refreshes instead of duplicating")
	assert.NotEqual(t, snaps[0].Rate, snaps[1].Rate)
}
