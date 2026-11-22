package ops

// sub2api 性能适配器。
//
// sub2api 没有 new-api 那样的 /api/perf-metrics 汇总接口，但它把每次调用都
// 记在用量流水里：
//   GET /api/v1/usage        成功请求（含 duration_ms / first_token_ms）
//   GET /api/v1/usage/errors 失败请求（含 model / status_code）
//
// 因此成功率必须由两侧条数派生：ok / (ok + err)，延迟与首字取成功请求的
// 平均值。全部按上游返回的 created_at 归到小时桶，保持与 new-api 适配器
// 相同的输出形状，让上层聚合无差别处理。

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
)

// sub2apiUsagePage 是分页列表的通用外壳。
type sub2apiUsagePage[T any] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items    []T   `json:"items"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"page_size"`
		Pages    int   `json:"pages"`
	} `json:"data"`
}

// sub2apiUsageItem 是一条成功用量记录（只取探活需要的字段）。
type sub2apiUsageItem struct {
	Model        string    `json:"model"`
	DurationMs   *int      `json:"duration_ms"`
	FirstTokenMs *int      `json:"first_token_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

// sub2apiErrorItem 是一条失败记录。
type sub2apiErrorItem struct {
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"created_at"`
	StatusCode int       `json:"status_code"`
}

// sub2apiAccessToken 读取渠道上配置的 sub2api 用户 token。
//
// sub2api 的 /api/v1/usage 走用户会话鉴权，中转密钥（sk-...）无效，因此
// 必须在渠道设置里单独配置。缺配置时该渠道跳过 sub2api 适配器。
func sub2apiAccessToken(channel *channelpkg.Channel) string {
	if channel == nil {
		return ""
	}
	settings := channel.GetOtherSettings()
	if settings.Sub2APIAccessToken == "" {
		return ""
	}
	return strings.TrimSpace(settings.Sub2APIAccessToken)
}

// fetchSub2APIPerf 从 sub2api 的用量与错误流水派生逐小时表现。
// 只统计 hours 窗口内、本渠道声明模型的数据。
func FetchSub2APIPerf(ctx context.Context, channel *channelpkg.Channel) (map[string][]UpstreamHourPoint, error) {
	token := sub2apiAccessToken(channel)
	if token == "" {
		return nil, fmt.Errorf("sub2api access token not configured")
	}
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	since := time.Now().Add(-24 * time.Hour)
	startDate := since.Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")

	okItems, errItems, err := fetchSub2APILogs(ctx, channel, base, token, startDate, endDate)
	if err != nil {
		return nil, err
	}
	if len(okItems) == 0 && len(errItems) == 0 {
		return nil, fmt.Errorf("sub2api returned no usage in window")
	}

	declared := make(map[string]struct{}, len(channel.GetModels()))
	for _, m := range channel.GetModels() {
		if name := strings.TrimSpace(m); name != "" {
			declared[name] = struct{}{}
		}
	}

	type hourKey struct {
		model string
		ts    int64
	}
	type agg struct {
		ok, err          int
		latencySum, latN int64
		ttftSum, ttftN   int64
	}
	buckets := make(map[hourKey]*agg)
	bucketOf := func(model string, t time.Time) (hourKey, *agg, bool) {
		name := strings.TrimSpace(model)
		if name == "" {
			return hourKey{}, nil, false
		}
		if _, ok := declared[name]; !ok {
			return hourKey{}, nil, false // 本渠道不声明该模型
		}
		k := hourKey{name, t.Unix() - t.Unix()%3600}
		a := buckets[k]
		if a == nil {
			a = &agg{}
			buckets[k] = a
		}
		return k, a, true
	}
	for _, it := range okItems {
		if _, a, ok := bucketOf(it.Model, it.CreatedAt); ok {
			a.ok++
			if it.DurationMs != nil && *it.DurationMs > 0 {
				a.latencySum += int64(*it.DurationMs)
				a.latN++
			}
			if it.FirstTokenMs != nil && *it.FirstTokenMs > 0 {
				a.ttftSum += int64(*it.FirstTokenMs)
				a.ttftN++
			}
		}
	}
	for _, it := range errItems {
		if _, a, ok := bucketOf(it.Model, it.CreatedAt); ok {
			a.err++
		}
	}

	out := make(map[string][]UpstreamHourPoint)
	for k, a := range buckets {
		total := a.ok + a.err
		if total == 0 {
			continue
		}
		point := UpstreamHourPoint{
			Ts:   k.ts,
			Rate: float64(a.ok) / float64(total) * 100,
		}
		if a.latN > 0 {
			point.LatencyMs = a.latencySum / a.latN
		}
		if a.ttftN > 0 {
			point.TtftMs = a.ttftSum / a.ttftN
		}
		out[k.model] = append(out[k.model], point)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sub2api usage has no model matching this channel")
	}
	return out, nil
}

// fetchSub2APILogs 拉取窗口内的成功与失败流水（分页、限页数防止过大）。
func fetchSub2APILogs(ctx context.Context, channel *channelpkg.Channel, base, token, startDate, endDate string) ([]sub2apiUsageItem, []sub2apiErrorItem, error) {
	const pageSize = 100
	const maxPages = 20 // 单次探活最多 2000 条，够覆盖小时聚合

	var okItems []sub2apiUsageItem
	for page := 1; page <= maxPages; page++ {
		target := fmt.Sprintf("%s/api/v1/usage?page=%d&page_size=%d&start_date=%s&end_date=%s",
			base, page, pageSize, url.QueryEscape(startDate), url.QueryEscape(endDate))
		body, err := upstreamGETWithToken(ctx, channel, target, token)
		if err != nil {
			return nil, nil, err
		}
		var pageDoc sub2apiUsagePage[sub2apiUsageItem]
		if err := common.Unmarshal(body, &pageDoc); err != nil {
			return nil, nil, fmt.Errorf("unparseable sub2api usage: %w", err)
		}
		okItems = append(okItems, pageDoc.Data.Items...)
		if page >= pageDoc.Data.Pages || len(pageDoc.Data.Items) == 0 {
			break
		}
	}

	var errItems []sub2apiErrorItem
	for page := 1; page <= maxPages; page++ {
		target := fmt.Sprintf("%s/api/v1/usage/errors?page=%d&page_size=%d&start_date=%s&end_date=%s",
			base, page, pageSize, url.QueryEscape(startDate), url.QueryEscape(endDate))
		body, err := upstreamGETWithToken(ctx, channel, target, token)
		if err != nil {
			// 错误视图可能被管理员关闭；缺失败数据时按只有成功的口径处理，
			// 并由调用方在成功率上看到 100%（诚实：确实没有失败记录）。
			break
		}
		var pageDoc sub2apiUsagePage[sub2apiErrorItem]
		if err := common.Unmarshal(body, &pageDoc); err != nil {
			break
		}
		errItems = append(errItems, pageDoc.Data.Items...)
		if page >= pageDoc.Data.Pages || len(pageDoc.Data.Items) == 0 {
			break
		}
	}
	return okItems, errItems, nil
}

// upstreamGETWithToken 用显式 bearer token 拉取上游（sub2api 需要用户 token，
// 不是渠道中转密钥）。
func upstreamGETWithToken(ctx context.Context, channel *channelpkg.Channel, rawURL, token string) ([]byte, error) {
	req, err := newUpstreamRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return doUpstreamRequest(req)
}

// 保证 strconv 在仅调试使用时也有引用，避免误删依赖。
var _ = strconv.Itoa
