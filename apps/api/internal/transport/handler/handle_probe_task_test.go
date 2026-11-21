package handler

// 回归：探活不再向上游发请求，而是把真实请求累积的健康状态窗口化成快照。
// 这里钉住判定口径 —— 看最近一次真实请求的成败，而不是被历史稀释的 EWMA
// 均值；否则一个刚刚连续失败的模型会因为过去成功太多而仍显示“可用”。

import (
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/stretchr/testify/assert"
)

func TestProbeResultForHealthUsesLatestOutcome(t *testing.T) {
	t.Run("latest success wins over an older error", func(t *testing.T) {
		ok, code := probeResultForHealth(channelpkg.ChannelModelHealth{
			EwmaScore:     0.2,
			LastSuccessAt: new(int64(200)),
			LastErrorAt:   new(int64(100)),
			LastErrorCode: "upstream_500",
		})
		assert.True(t, ok, "a newer success means the model is usable now")
		assert.Empty(t, code)
	})

	t.Run("latest error wins over an older success", func(t *testing.T) {
		ok, code := probeResultForHealth(channelpkg.ChannelModelHealth{
			EwmaScore:     0.95,
			LastSuccessAt: new(int64(100)),
			LastErrorAt:   new(int64(200)),
			LastErrorCode: "upstream_500",
		})
		assert.False(t, ok, "a high EWMA must not mask a newer failure")
		assert.Equal(t, "upstream_500", code)
	})

	t.Run("success only", func(t *testing.T) {
		ok, code := probeResultForHealth(channelpkg.ChannelModelHealth{
			LastSuccessAt: new(int64(100)),
		})
		assert.True(t, ok)
		assert.Empty(t, code)
	})

	t.Run("warm-up row with no outcome is not a failure", func(t *testing.T) {
		ok, code := probeResultForHealth(channelpkg.ChannelModelHealth{})
		assert.True(t, ok)
		assert.Empty(t, code)
	})
}
