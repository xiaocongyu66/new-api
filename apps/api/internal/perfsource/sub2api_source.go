package perfsource

// sub2api 上游：GET /api/v1/usage（成功流水）与 GET /api/v1/usage/errors（失败流水）。
//
// sub2api 没有 new-api 那样的汇总性能接口，但它把每次调用都记在用量流水里：
// 成功流水带 duration_ms / first_token_ms，失败流水带 model / status_code。
// 因此成功率必须由两侧条数派生：ok / (ok + err)，延迟与首字取成功请求的
// 平均值。按上游返回的 created_at 归到小时桶，输出形状与 new-api 一致。
//
// 鉴权：这两个接口走用户会话鉴权，中转密钥（sk-...）在那里无效
// （返回 INVALID_TOKEN），所以必须用渠道设置里单独配置的 access token。

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
)

type sub2APISource struct{}

func (sub2APISource) Name() string { return "sub2api" }

// usagePage 是分页列表的通用外壳。
type usagePage[T any] struct {
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

// usageItem 是一条成功用量记录（只取探活需要的字段）。
type usageItem struct {
	Model        string    `json:"model"`
	DurationMs   *int      `json:"duration_ms"`
	FirstTokenMs *int      `json:"first_token_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

// errorItem 是一条失败记录。
type errorItem struct {
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"created_at"`
	StatusCode int       `json:"status_code"`
}

// accessToken 读取渠道上配置的 sub2api 用户 token。缺配置时该级直接失败。
func accessToken(channel *channelpkg.Channel) string {
	if channel == nil {
		return ""
	}
	return strings.TrimSpace(channel.GetOtherSettings().Sub2APIAccessToken)
}

func (sub2APISource) Fetch(ctx context.Context, channel *channelpkg.Channel) (map[string][]Point, error) {
	token := accessToken(channel)
	if token == "" {
		return nil, fmt.Errorf("sub2api access token not configured")
	}
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	startDate := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")

	okItems, errItems, err := fetchUsageLogs(ctx, base, token, startDate, endDate)
	if err != nil {
		return nil, err
	}
	if len(okItems) == 0 && len(errItems) == 0 {
		return nil, fmt.Errorf("sub2api returned no usage in window")
	}

	declared := declaredModels(channel)
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

	out := make(map[string][]Point)
	for k, a := range buckets {
		total := a.ok + a.err
		if total == 0 {
			continue
		}
		point := Point{Ts: k.ts, Rate: float64(a.ok) / float64(total) * 100}
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

// fetchUsageLogs 拉取窗口内的成功与失败流水（分页、限页数防止过大）。
func fetchUsageLogs(ctx context.Context, base, token, startDate, endDate string) ([]usageItem, []errorItem, error) {
	const pageSize = 100
	const maxPages = 20 // 单次探活最多 2000 条，够覆盖小时聚合

	var okItems []usageItem
	for page := 1; page <= maxPages; page++ {
		target := fmt.Sprintf("%s/api/v1/usage?page=%d&page_size=%d&start_date=%s&end_date=%s",
			base, page, pageSize, url.QueryEscape(startDate), url.QueryEscape(endDate))
		body, err := getWithBearer(ctx, target, token)
		if err != nil {
			return nil, nil, err
		}
		var pageDoc usagePage[usageItem]
		if err := common.Unmarshal(body, &pageDoc); err != nil {
			return nil, nil, fmt.Errorf("unparseable sub2api usage: %w", err)
		}
		okItems = append(okItems, pageDoc.Data.Items...)
		if page >= pageDoc.Data.Pages || len(pageDoc.Data.Items) == 0 {
			break
		}
	}

	var errItems []errorItem
	for page := 1; page <= maxPages; page++ {
		target := fmt.Sprintf("%s/api/v1/usage/errors?page=%d&page_size=%d&start_date=%s&end_date=%s",
			base, page, pageSize, url.QueryEscape(startDate), url.QueryEscape(endDate))
		body, err := getWithBearer(ctx, target, token)
		if err != nil {
			// 失败流水是成功率的分母不可分割的一半。看不到失败记录时无法
			// 区分「真的零失败」和「错误视图被关闭」，此时算出的成功率是
			// 伪造的 100%。宁可让这一级失败、继续回退，也不编造。
			return nil, nil, fmt.Errorf("sub2api error log unavailable: %w", err)
		}
		var pageDoc usagePage[errorItem]
		if err := common.Unmarshal(body, &pageDoc); err != nil {
			return nil, nil, fmt.Errorf("unparseable sub2api error log: %w", err)
		}
		errItems = append(errItems, pageDoc.Data.Items...)
		if page >= pageDoc.Data.Pages || len(pageDoc.Data.Items) == 0 {
			break
		}
	}
	return okItems, errItems, nil
}
