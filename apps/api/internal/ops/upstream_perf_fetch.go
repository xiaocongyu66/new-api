package ops

// 上游性能取数：探活调用上游站暴露的性能接口（new-api 的 /api/perf-metrics/*），
// 拿上游自己统计的逐小时模型表现。零推理消耗，数据来自上游真实流量。
//
// 关键点：上游返回的是「最近 N 小时里每小时一个点」的序列，不是单个汇总标量。
// 只有按 ts 逐点落库，图表才可能显示真实起伏；把 24h 汇总值平铺到每小时
// 会得到一条毫无意义的直线。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/egress"
)

const upstreamPerfTimeout = 15 * time.Second

// UpstreamHourPoint 是上游某模型在某一个小时的表现。
type UpstreamHourPoint struct {
	Ts        int64   // 上游的小时桶起点（unix 秒）
	Rate      float64 // 0-100
	LatencyMs int64   // 0 = 上游未给出
	TtftMs    int64   // 0 = 上游未给出
}

// upstreamPerfSnapshot 是一次上游取数：本地模型别名 → 逐小时序列。
type upstreamPerfSnapshot struct {
	Models map[string][]UpstreamHourPoint
}

// upstreamAliasMap 读取渠道的路由映射：上游模型名 → 本地公开别名。
// 探活要按上游返回的名字反查我们自己的模型名，否则配了 model_mapping
// 的渠道一个都对不上。
func upstreamAliasMap(channelId int) (map[string]string, error) {
	var rows []struct {
		UpstreamModel    string
		PublicModelAlias string
	}
	if err := dbx.DB.Table("channel_model_routes AS r").
		Select("r.upstream_model, r.public_model_alias").
		Where("r.channel_id = ? AND r.enabled = ?", channelId, true).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		up := strings.TrimSpace(r.UpstreamModel)
		alias := strings.TrimSpace(r.PublicModelAlias)
		if up != "" && alias != "" {
			out[up] = alias
		}
	}
	return out, nil
}

// newUpstreamRequest 构造一个带 SSRF 校验与超时的上游 GET 请求。
func newUpstreamRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	if err := egress.ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamPerfTimeout)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	// 请求随 ctx 结束；cancel 由 ctx 的 deadline 兜住，这里不提前释放。
	_ = cancel
	return req, nil
}

// doUpstreamRequest 发送请求并读取受限长度响应体。
func doUpstreamRequest(req *http.Request) ([]byte, error) {
	// GetSSRFProtectedHTTPClient 在启动初始化前可能为 nil（例如后台任务先于
	// InitHttpClient 启动）。裸调用会 panic，这里兜底到一个带超时的客户端，
	// 初始 URL 已通过 ValidateSSRFProtectedFetchURL 校验。
	client := egress.GetSSRFProtectedHTTPClient()
	if client == nil {
		client = &http.Client{Timeout: upstreamPerfTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("upstream has no perf endpoint: %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream perf status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// upstreamGET 发送一次带渠道密钥的上游 GET（new-api 形态）。
func upstreamGET(ctx context.Context, channel *channelpkg.Channel, rawURL string) ([]byte, error) {
	req, err := newUpstreamRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if key, _, keyErr := channel.GetNextEnabledKey(""); keyErr == nil && strings.TrimSpace(key) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	}
	return doUpstreamRequest(req)
}

// FetchUpstreamPerf 调用上游站性能接口，返回 本地模型别名 → 逐小时序列。
//
// 上游 summary 给出模型清单与逐小时成功率；单模型接口的 series 更完整
// （同时带 avg_ttft_ms 与 avg_latency_ms），所以对清单里的每个模型再取一次。
// 只取能映射到本渠道声明模型的上游模型，避免无谓请求。
func FetchUpstreamPerf(ctx context.Context, channel *channelpkg.Channel) (*upstreamPerfSnapshot, error) {
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	aliases, err := upstreamAliasMap(channel.Id)
	if err != nil {
		return nil, err
	}
	declared := make(map[string]struct{}, len(channel.GetModels()))
	for _, m := range channel.GetModels() {
		if name := strings.TrimSpace(m); name != "" {
			declared[name] = struct{}{}
		}
	}

	// 1) summary：拿到上游的模型清单（名字用于逐个取序列）。
	body, err := upstreamGET(ctx, channel, base+"/api/perf-metrics/summary?hours=24")
	if err != nil {
		return nil, err
	}
	var summary struct {
		Success bool `json:"success"`
		Data    struct {
			Models []struct {
				ModelName   string  `json:"model_name"`
				SuccessRate float64 `json:"success_rate"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("unparseable perf summary: %w", err)
	}

	// 2) 逐个模型取逐小时序列（含 TTFT 与延迟）。
	out := &upstreamPerfSnapshot{Models: make(map[string][]UpstreamHourPoint)}
	for _, m := range summary.Data.Models {
		upstreamName := strings.TrimSpace(m.ModelName)
		if upstreamName == "" {
			continue
		}
		alias, ok := aliases[upstreamName]
		if !ok {
			alias = upstreamName // 未配置映射时按同名匹配
		}
		if _, ok := declared[alias]; !ok {
			continue // 本渠道不声明这个模型，跳过
		}
		points, pErr := fetchUpstreamModelSeries(ctx, channel, base, upstreamName)
		if pErr != nil || len(points) == 0 {
			common.SysLog(fmt.Sprintf("upstream series unavailable channel=%d model=%q err=%v points=%d", channel.Id, upstreamName, pErr, len(points)))
			// 单模型接口不可用时退回 summary 的 success_rate 单点，
			// 至少保留该模型最近窗口的汇总值。
			if m.SuccessRate >= 0 {
				out.Models[alias] = []UpstreamHourPoint{{Ts: 0, Rate: m.SuccessRate}}
			}
			continue
		}
		out.Models[alias] = points
	}
	if len(out.Models) == 0 {
		return nil, fmt.Errorf("upstream returned no model matching this channel")
	}
	return out, nil
}

// fetchUpstreamModelSeries 读取单模型接口的逐小时 series。
func fetchUpstreamModelSeries(ctx context.Context, channel *channelpkg.Channel, base, model string) ([]UpstreamHourPoint, error) {
	target := base + "/api/perf-metrics?model=" + url.QueryEscape(model) + "&hours=24"
	body, err := upstreamGET(ctx, channel, target)
	if err != nil {
		return nil, err
	}
	points, err := parseUpstreamModelSeries(body)
	if err != nil {
		return nil, err
	}
	return points, nil
}

// parseUpstreamModelSeries 解析单模型接口的逐小时序列。抽成纯函数以便单测：
// HTTP/SSRF 之外的解析逻辑必须能独立验证。
func parseUpstreamModelSeries(body []byte) ([]UpstreamHourPoint, error) {
	var doc struct {
		Success bool `json:"success"`
		Data    struct {
			Series []struct {
				Ts         int64    `json:"ts"`
				AvgTtftMs  *float64 `json:"avg_ttft_ms"`
				AvgLatency *float64 `json:"avg_latency_ms"`
				SuccessPct *float64 `json:"success_rate"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("unparseable model series: %w", err)
	}
	points := make([]UpstreamHourPoint, 0, len(doc.Data.Series))
	for _, p := range doc.Data.Series {
		if p.Ts <= 0 || p.SuccessPct == nil {
			continue // 没有时间桶或成功率的点无法落到具体小时
		}
		point := UpstreamHourPoint{Ts: p.Ts, Rate: *p.SuccessPct}
		if p.AvgLatency != nil && *p.AvgLatency > 0 {
			point.LatencyMs = int64(*p.AvgLatency)
		}
		if p.AvgTtftMs != nil && *p.AvgTtftMs > 0 {
			point.TtftMs = int64(*p.AvgTtftMs)
		}
		points = append(points, point)
	}
	return points, nil
}
