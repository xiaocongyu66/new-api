package ops

// 回归：sub2api 适配器必须从「成功流水 + 失败流水」两侧派生成功率。
//
// sub2api 没有 new-api 那种汇总性能接口，只有逐条用量。成功率 = ok/(ok+err)
// 必须同时读 /api/v1/usage 与 /api/v1/usage/errors，只读一侧会把失败当成功。

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/egress"
)

func unmarshalForTest(body []byte, v any) error { return common.Unmarshal(body, v) }

var _ = time.Now

func TestSub2APIUsagePageParsing(t *testing.T) {
	body := []byte(`{
	  "code": "OK",
	  "data": {
	    "items": [
	      {"model": "gpt-5.6-sol", "duration_ms": 4200, "first_token_ms": 900, "created_at": "2026-11-22T10:05:00Z"},
	      {"model": "gpt-5.6-sol", "duration_ms": 7100, "first_token_ms": 1500, "created_at": "2026-11-22T10:35:00Z"}
	    ],
	    "total": 2, "page": 1, "page_size": 100, "pages": 1
	  }
	}`)
	var page sub2apiUsagePage[sub2apiUsageItem]
	require.NoError(t, unmarshalForTest(body, &page))
	require.Len(t, page.Data.Items, 2)
	assert.Equal(t, "gpt-5.6-sol", page.Data.Items[0].Model)
	require.NotNil(t, page.Data.Items[0].DurationMs)
	assert.Equal(t, 4200, *page.Data.Items[0].DurationMs)
	require.NotNil(t, page.Data.Items[0].FirstTokenMs)
	assert.Equal(t, 900, *page.Data.Items[0].FirstTokenMs)
	assert.Equal(t, 1, page.Data.Pages)

	// Both records land in the same hour bucket → one hourly point.
	hour := page.Data.Items[0].CreatedAt.Unix() - page.Data.Items[0].CreatedAt.Unix()%3600
	assert.Equal(t, hour, page.Data.Items[1].CreatedAt.Unix()-page.Data.Items[1].CreatedAt.Unix()%3600)
}

func TestSub2APIRateDerivation(t *testing.T) {
	// ok=3, err=1 → 75%, not 100%.
	ok, failed := 3, 1
	rate := float64(ok) / float64(ok+failed) * 100
	assert.InDelta(t, 75, rate, 0.001)

	// No failures recorded → 100% (honest: nothing failed).
	rate = float64(5) / float64(5+0) * 100
	assert.InDelta(t, 100, rate, 0.001)
}

func TestSub2APITokensAreNotRelayKeys(t *testing.T) {
	// The channel's relay key (sk-...) is rejected by /api/v1/usage; the
	// adapter must require an explicitly configured access token instead.
	assert.Equal(t, "", sub2apiAccessToken(nil))
}

// TestFetchSub2APIPerfEndToEnd 用假 sub2api 跑完整路径：
// 成功流水给 duration/first_token，失败流水给错误条数，成功率必须由两侧
// 派生（3 成 1 败 = 75%），且按小时归并、按渠道声明模型过滤。
func TestFetchSub2APIPerfEndToEnd(t *testing.T) {
	withAvailabilityDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"OK","data":{"items":[
			{"model":"gpt-5.6-sol","duration_ms":4000,"first_token_ms":900,"created_at":"2026-11-22T10:05:00Z"},
			{"model":"gpt-5.6-sol","duration_ms":6000,"first_token_ms":1100,"created_at":"2026-11-22T10:35:00Z"},
			{"model":"gpt-5.6-sol","duration_ms":8000,"first_token_ms":1300,"created_at":"2026-11-22T10:55:00Z"},
			{"model":"not-declared","duration_ms":1000,"first_token_ms":100,"created_at":"2026-11-22T10:10:00Z"}
		],"total":4,"page":1,"page_size":100,"pages":1}}`))
	})
	mux.HandleFunc("/api/v1/usage/errors", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"OK","data":{"items":[
			{"model":"gpt-5.6-sol","status_code":500,"created_at":"2026-11-22T10:20:00Z"}
		],"total":1,"page":1,"page_size":100,"pages":1}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fetchSetting := egress.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() { *fetchSetting = original })
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = nil

	channel := &channelpkg.Channel{
		Id: 6, Type: 1, Key: "sk-relay", Name: "xcmapi",
		Models: "gpt-5.6-sol", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "tok-123"
	channel.SetOtherSettings(settings)

	perf, err := FetchSub2APIPerf(t.Context(), channel)
	require.NoError(t, err)
	points, ok := perf["gpt-5.6-sol"]
	require.True(t, ok)
	require.Len(t, points, 1, "all records share one hour bucket")

	// 3 successes + 1 error -> 75%, not 100%.
	assert.InDelta(t, 75, points[0].Rate, 0.001)
	// (4000+6000+8000)/3 = 6000
	assert.Equal(t, int64(6000), points[0].LatencyMs)
	// (900+1100+1300)/3 = 1100
	assert.Equal(t, int64(1100), points[0].TtftMs)

	_, notDeclared := perf["not-declared"]
	assert.False(t, notDeclared, "models this channel does not declare are skipped")
}

// TestFetchSub2APIPerfRefusesWithoutErrorLog 钉住诚实性契约：
// 失败流水读不到时，无法区分「真的零失败」与「错误视图被关闭」，
// 此时不能输出 100% 这样的伪造值，必须让该级失败以便回退到本地真实请求。
func TestFetchSub2APIPerfRefusesWithoutErrorLog(t *testing.T) {
	withAvailabilityDB(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"OK","data":{"items":[
			{"model":"m","duration_ms":1000,"first_token_ms":100,"created_at":"2026-11-22T10:05:00Z"}
		],"total":1,"page":1,"page_size":100,"pages":1}}`))
	})
	// Error view disabled by the upstream admin.
	mux.HandleFunc("/api/v1/usage/errors", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"FORBIDDEN","message":"Error requests view is disabled"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fetchSetting := egress.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() { *fetchSetting = original })
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = nil

	channel := &channelpkg.Channel{
		Id: 9, Type: 1, Key: "sk-relay", Name: "x",
		Models: "m", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "tok"
	channel.SetOtherSettings(settings)

	perf, err := FetchSub2APIPerf(t.Context(), channel)
	require.Error(t, err, "an unavailable error log must fail the tier, not fake 100%")
	assert.Nil(t, perf)
	assert.Contains(t, err.Error(), "error log unavailable")
}
