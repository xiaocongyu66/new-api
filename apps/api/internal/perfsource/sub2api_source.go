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
//
// 不声明 code/message：这两个字段从未被使用，而 sub2api 各接口的 code 类型
// 并不统一（用量接口返回数字 0，登录接口也是数字）。声明成 string 会让
// json 解码直接失败，把「这一级可用」误判成「这一级不可用」。
type usagePage[T any] struct {
	Data struct {
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
	// 取数与鉴权必须共用同一个 base。凭据解析会把 base 归一化成 origin
	// （去掉 base_url 上的面板路径），这里若再用渠道原值，带路径的渠道就会
	// 出现「登录成功、/api/v1/usage 404」。
	credentials, err := sub2apiCredentials(channel)
	if err != nil {
		return nil, err
	}
	token, err := resolveSub2APIToken(ctx, channel)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(credentials.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	startDate := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	endDate := time.Now().Format("2006-01-02")

	okItems, errItems, err := fetchUsageLogs(ctx, base, token, startDate, endDate)
	if err != nil && isSub2APIAuthError(err) {
		// 本地 token 被上游拒绝（过期/被吊销）：先作废旧值，再强制续期一次。
		// 只重试一次，避免上游持续 401 时反复登录触发风控。
		invalidateSub2APIToken(channel)
		token, err = forceRefreshSub2APIToken(ctx, channel)
		if err != nil {
			return nil, fmt.Errorf("sub2api auth refresh after unauthorized response failed: %w", err)
		}
		okItems, errItems, err = fetchUsageLogs(ctx, base, token, startDate, endDate)
	}
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

// isSub2APIAuthError 判断取数错误是否来自上游 401/403。
// upstreamfetch.Get 把状态码折叠进错误文本：401 是 "upstream status 401"，
// 403 与 404 共用 "upstream has no such endpoint: N"。sub2api 的成功流水
// 接口只在鉴权失败时返回 403，因此那里把 403 视为需要续期的信号。
// 但失败流水接口的 403 还可能是管理员关闭了「错误请求」视图，换 token
// 也打不开，必须保持「错误流水不可用」的原语义，不能当成鉴权失败重试。
func isSub2APIAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "upstream status 401") || strings.Contains(msg, "upstream status 403") {
		return true
	}
	if strings.Contains(msg, "upstream has no such endpoint: 403") {
		return !strings.Contains(msg, "sub2api error log unavailable")
	}
	return false
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
