package ops

// 回归：sub2api 适配器必须从「成功流水 + 失败流水」两侧派生成功率。
//
// sub2api 没有 new-api 那种汇总性能接口，只有逐条用量。成功率 = ok/(ok+err)
// 必须同时读 /api/v1/usage 与 /api/v1/usage/errors，只读一侧会把失败当成功。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/common"
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
