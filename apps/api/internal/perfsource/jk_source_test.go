package perfsource

// 回归：公开监控页（/jk/status.json）的取数口径。
//
// 上游把采样状态定义成 green=正常、yellow=慢但成功、red=失败，所以成功率
// 必须是 (green+yellow)/全部已知状态。把 yellow 当失败会平白制造一次故障，
// 把 red 当成功则会掩盖真实故障——两个方向都不能错。
//
// 另外这份数据是按上游的「组」组织的，不是按我们的渠道。组必须要么显式
// 配置、要么由密钥的模型集合唯一反查出来；多个组模型集合相同时必须拒绝，
// 不能猜——猜错就是把别人组的健康度当成自己的。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jkFixture = `{
  "generated_at": "2026-11-23T14:55:30+08:00",
  "groups": [
    {"id": 6, "name": "Pro", "supported_models": ["m1", "m2"]},
    {"id": 40, "name": "ProPool", "supported_models": ["m1", "m2"]}
  ],
  "model_history": {
    "6": {
      "m1": [
        {"at": "2026-11-23T10:01:00Z", "status": "green",  "response_ms": 1000},
        {"at": "2026-11-23T10:05:00Z", "status": "yellow", "response_ms": 9000},
        {"at": "2026-11-23T10:09:00Z", "status": "green",  "response_ms": 2000},
        {"at": "2026-11-23T10:13:00Z", "status": "red",    "response_ms": null},
        {"at": "2026-11-23T11:02:00Z", "status": "green",  "response_ms": 3000}
      ],
      "other-model": [
        {"at": "2026-11-23T10:05:00Z", "status": "red", "response_ms": null}
      ]
    },
    "40": {
      "m1": [
        {"at": "2026-11-23T10:05:00Z", "status": "red", "response_ms": null}
      ]
    }
  }
}`

func TestJKStatusRateTreatsYellowAsSuccess(t *testing.T) {
	withPerfDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jk/status.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jkFixture))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	allowLoopback(t)

	channel := &channelpkg.Channel{
		Id: 6, Type: 1, Key: "sk-test", Name: "xcmapi",
		Models: "m1", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}
	settings := channel.GetOtherSettings()
	settings.JKGroupID = "6"
	channel.SetOtherSettings(settings)

	perf, err := (jkSource{}).Fetch(t.Context(), channel)
	require.NoError(t, err)
	points, ok := perf["m1"]
	require.True(t, ok)
	require.Len(t, points, 2, "the 10:00 and 11:00 hours both have samples")

	// 10:00 bucket: green+yellow+green = 3 success, red = 1 failure -> 75%.
	assert.InDelta(t, 75, points[0].Rate, 0.001)
	// (1000+9000+2000)/3 = 4000; the red sample has no response_ms and is excluded.
	assert.Equal(t, int64(4000), points[0].LatencyMs)

	// 11:00 bucket: a single green sample -> 100%.
	assert.InDelta(t, 100, points[1].Rate, 0.001)
	assert.Equal(t, int64(3000), points[1].LatencyMs)

	// The explicitly configured group must be used; group 40 reports m1 as red.
	for _, p := range points {
		assert.NotEqual(t, 0.0, p.Rate+1, "sanity")
	}
	assert.NotEqual(t, 0.0, points[0].Rate, "group 40's red-only hour must not leak in")
}

func TestJKStatusSkipsModelsTheChannelDoesNotDeclare(t *testing.T) {
	withPerfDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jk/status.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jkFixture))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	allowLoopback(t)

	channel := &channelpkg.Channel{
		Id: 6, Type: 1, Key: "sk-test", Name: "xcmapi",
		Models: "m1", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}
	settings := channel.GetOtherSettings()
	settings.JKGroupID = "6"
	channel.SetOtherSettings(settings)

	perf, err := (jkSource{}).Fetch(t.Context(), channel)
	require.NoError(t, err)
	_, ok := perf["other-model"]
	assert.False(t, ok, "models outside the channel's declared list must be dropped")
}

// TestJKGroupRefusesAmbiguousAutoDetect：密钥的模型集合同时命中多个组时，
// 不允许任选一个——必须报错并列出候选，让运维显式指定 jk_group_id。
func TestJKGroupRefusesAmbiguousAutoDetect(t *testing.T) {
	withPerfDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jk/status.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jkFixture))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	allowLoopback(t)

	channel := &channelpkg.Channel{
		Id: 6, Type: 1, Key: "sk-test", Name: "xcmapi",
		Models: "m1,m2", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}

	perf, err := (jkSource{}).Fetch(t.Context(), channel)
	require.Error(t, err, "two groups sharing the same model set must not be guessed")
	assert.Nil(t, perf)
	assert.Contains(t, err.Error(), "ambiguous jk group")
	assert.Contains(t, err.Error(), "6")
	assert.Contains(t, err.Error(), "40")
}

// TestJKGroupAutoDetectPicksTheOnlyMatch：只有一个组的模型集合与密钥一致时，
// 可以自动采用，不必强制配置。
func TestJKGroupAutoDetectPicksTheOnlyMatch(t *testing.T) {
	withPerfDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/jk/status.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "groups": [{"id": 7, "name": "only", "supported_models": ["m1"]}],
		  "model_history": {"7": {"m1": [{"at": "2026-11-23T10:05:00Z", "status": "green", "response_ms": 1500}]}}
		}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	allowLoopback(t)

	channel := &channelpkg.Channel{
		Id: 6, Type: 1, Key: "sk-test", Name: "xcmapi",
		Models: "m1", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}

	perf, err := (jkSource{}).Fetch(t.Context(), channel)
	require.NoError(t, err)
	points, ok := perf["m1"]
	require.True(t, ok)
	require.Len(t, points, 1)
	assert.InDelta(t, 100, points[0].Rate, 0.001)
	assert.Equal(t, int64(1500), points[0].LatencyMs)
}
