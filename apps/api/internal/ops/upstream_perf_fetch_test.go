package ops

// 回归：上游性能接口的解析必须保留逐小时差异。
//
// 这是图表平线的直接成因：上游返回的是每小时一个点，只取汇总标量再平铺
// 会得到一条毫无意义的直线。以下测试钉住 ts / 成功率 / TTFT / 延迟都按点
// 保留，缺失字段落在 0 而不是伪造。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const upstreamSeriesFixture = `{
  "success": true,
  "data": {
    "model_name": "gemini-3.8-flash",
    "summary": {"avg_latency_ms": 30548, "success_rate": 96.58, "avg_tps": 68.26},
    "series": [
      {"ts": 1795284000, "avg_ttft_ms": 10538, "avg_latency_ms": 78595, "success_rate": 71.42857142857143, "avg_tps": 0},
      {"ts": 1795287600, "avg_ttft_ms": 3200,  "avg_latency_ms": 30210, "success_rate": 47.37, "avg_tps": 12},
      {"ts": 1795291200, "avg_ttft_ms": null,  "avg_latency_ms": null,  "success_rate": 0, "avg_tps": 0},
      {"ts": 0,          "avg_ttft_ms": 1,     "avg_latency_ms": 1,     "success_rate": 99, "avg_tps": 0}
    ]
  }
}`

func TestParseUpstreamModelSeriesKeepsHourlyPoints(t *testing.T) {
	points, err := parseUpstreamModelSeries([]byte(upstreamSeriesFixture))
	require.NoError(t, err)
	require.Len(t, points, 3, "the point without ts must be dropped")

	assert.Equal(t, int64(1795284000), points[0].Ts)
	assert.InDelta(t, 71.43, points[0].Rate, 0.01)
	assert.Equal(t, int64(78595), points[0].LatencyMs)
	assert.Equal(t, int64(10538), points[0].TtftMs)

	// Each hour carries its own values — the whole point of the series.
	assert.NotEqual(t, points[0].Rate, points[1].Rate)
	assert.NotEqual(t, points[0].LatencyMs, points[1].LatencyMs)
	assert.NotEqual(t, points[0].TtftMs, points[1].TtftMs)

	// A failed hour with no measurements keeps rate 0 and no latency/TTFT.
	assert.InDelta(t, 0, points[2].Rate, 0.001)
	assert.Zero(t, points[2].LatencyMs)
	assert.Zero(t, points[2].TtftMs)
}

func TestParseUpstreamModelSeriesRejectsGarbage(t *testing.T) {
	_, err := parseUpstreamModelSeries([]byte("not json"))
	require.Error(t, err)
}

// TestFetchUpstreamPerfEndToEnd 用一个假上游跑完整取数路径：
// summary 给模型清单，单模型接口给逐小时序列；配置 model_mapping 时
// 上游名必须映射回本地公开别名。这是图表数据的唯一来源，必须有端到端覆盖。
func TestFetchUpstreamPerfEndToEnd(t *testing.T) {
	withAvailabilityDB(t)

	// A fake upstream exposing both endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/perf-metrics/summary", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"models":[
			{"model_name":"upstream-alias","success_rate":55.5},
			{"model_name":"not-declared","success_rate":90}
		]}}`))
	})
	mux.HandleFunc("/api/perf-metrics", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("model"); got != "upstream-alias" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"series":[
			{"ts":1795284000,"avg_ttft_ms":1200,"avg_latency_ms":8000,"success_rate":71.43},
			{"ts":1795287600,"avg_ttft_ms":900,"avg_latency_ms":5000,"success_rate":47.37}
		]}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Allow the loopback address so the SSRF-guarded client can reach the fake.
	fetchSetting := egress.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() { *fetchSetting = original })
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = nil // httptest uses an ephemeral port

	// local alias "gpt-4" is served by upstream "upstream-alias".
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelRoute{
		Id: 1, PublicModelAlias: "gpt-4", ChannelId: 42, KeyIndex: 0,
		UpstreamModel: "upstream-alias", StaticWeight: 5, Enabled: true,
	}).Error)

	channel := &channelpkg.Channel{
		Id: 42, Type: 1, Key: "sk-test", Name: "fake",
		Models: "gpt-4", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}

	snap, err := FetchUpstreamPerf(t.Context(), channel)
	require.NoError(t, err)
	points, ok := snap.Models["gpt-4"]
	require.True(t, ok, "the upstream name must map back to the local alias")
	require.Len(t, points, 2)
	assert.InDelta(t, 71.43, points[0].Rate, 0.01)
	assert.Equal(t, int64(8000), points[0].LatencyMs)
	assert.Equal(t, int64(1200), points[0].TtftMs)
	assert.InDelta(t, 47.37, points[1].Rate, 0.01)
	assert.NotEqual(t, points[0].Rate, points[1].Rate, "hourly values must stay distinct")

	_, notDeclared := snap.Models["not-declared"]
	assert.False(t, notDeclared, "models this channel does not declare must be skipped")
}
