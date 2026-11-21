package handler

// 回归：探活不向上游发任何请求，而是把真实请求的最新结果窗口化成快照。
// 这里钉住判定口径 —— 每个 (channel, model) 以最新一条日志为准，成功是
// consume 日志、失败是 error 日志。历史成功不能掩盖刚刚发生的失败。

import (
	"testing"

	usagedomain "github.com/QuantumNous/new-api/internal/usage"
	"github.com/stretchr/testify/assert"
)

func TestLatestProbeOutcomes(t *testing.T) {
	t.Run("newest consume wins over older error", func(t *testing.T) {
		// Rows arrive newest-first, matching the ORDER BY created_at DESC query.
		out := latestProbeOutcomes([]probeLogRow{
			{ChannelId: 1, ModelName: "m", Type: usagedomain.LogTypeConsume, UseTime: 2},
			{ChannelId: 1, ModelName: "m", Type: usagedomain.LogTypeError, Content: "upstream_500"},
		})
		got := out[probeUnitKey{ChannelId: 1, Model: "m"}]
		assert.True(t, got.Success, "a newer success means the model is usable now")
		assert.Equal(t, int64(2000), got.LatencyMs)
		assert.Empty(t, got.ErrorCode)
	})

	t.Run("newest error wins over older consume", func(t *testing.T) {
		out := latestProbeOutcomes([]probeLogRow{
			{ChannelId: 1, ModelName: "m", Type: usagedomain.LogTypeError, Content: "upstream_500"},
			{ChannelId: 1, ModelName: "m", Type: usagedomain.LogTypeConsume, UseTime: 2},
		})
		got := out[probeUnitKey{ChannelId: 1, Model: "m"}]
		assert.False(t, got.Success, "a newer failure must not be masked by older successes")
		assert.Equal(t, "upstream_500", got.ErrorCode)
	})

	t.Run("units stay independent", func(t *testing.T) {
		out := latestProbeOutcomes([]probeLogRow{
			{ChannelId: 1, ModelName: "ok", Type: usagedomain.LogTypeConsume, UseTime: 1},
			{ChannelId: 1, ModelName: "bad", Type: usagedomain.LogTypeError, Content: "boom"},
			{ChannelId: 2, ModelName: "ok", Type: usagedomain.LogTypeError, Content: "boom"},
		})
		assert.True(t, out[probeUnitKey{ChannelId: 1, Model: "ok"}].Success)
		assert.False(t, out[probeUnitKey{ChannelId: 1, Model: "bad"}].Success)
		assert.False(t, out[probeUnitKey{ChannelId: 2, Model: "ok"}].Success,
			"the same model on another channel is a separate unit")
	})

	t.Run("blank rows are ignored", func(t *testing.T) {
		out := latestProbeOutcomes([]probeLogRow{
			{ChannelId: 0, ModelName: "m", Type: usagedomain.LogTypeConsume},
			{ChannelId: 1, ModelName: "  ", Type: usagedomain.LogTypeConsume},
		})
		assert.Empty(t, out)
	})
}
