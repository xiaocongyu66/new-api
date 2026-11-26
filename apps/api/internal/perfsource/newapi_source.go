package perfsource

// new-api 上游：GET /api/perf-metrics/summary 给模型清单，
// GET /api/perf-metrics?model=X 给该模型的逐小时序列。
// 零推理消耗，数据来自上游自己的真实流量统计。
//
// 关键点：上游返回的是「最近 N 小时里每小时一个点」的序列，不是汇总标量。
// 只有按 ts 逐点保留，图表才可能显示真实起伏；把 24h 汇总值平铺到每小时
// 会得到一条毫无意义的直线。

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
)

type newAPISource struct{}

func (newAPISource) Name() string { return "newapi" }

// Fetch 调用上游性能接口，返回 本地模型别名 → 逐小时序列。
//
// summary 给出模型清单；单模型接口的 series 更完整（同时带 avg_ttft_ms 与
// avg_latency_ms），所以对清单里的每个模型再取一次。只取能映射到本渠道
// 声明模型的上游模型，避免无谓请求。
func (newAPISource) Fetch(ctx context.Context, channel *channelpkg.Channel) (map[string][]Point, error) {
	base := channelOrigin(channel)
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	aliases, err := aliasMap(channel.Id)
	if err != nil {
		return nil, err
	}
	declared := declaredModels(channel)

	// 1) summary：拿到上游的模型清单（名字用于逐个取序列）。
	body, err := getWithChannelKey(ctx, channel, base+"/api/perf-metrics/summary?hours=24")
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
	out := make(map[string][]Point)
	for _, m := range summary.Data.Models {
		upstreamName := strings.TrimSpace(m.ModelName)
		if upstreamName == "" {
			continue
		}
		alias := localAlias(aliases, upstreamName)
		if _, ok := declared[alias]; !ok {
			continue // 本渠道不声明这个模型，跳过
		}
		points, pErr := fetchModelSeries(ctx, channel, base, upstreamName)
		if pErr != nil || len(points) == 0 {
			common.SysLog(fmt.Sprintf("upstream series unavailable channel=%d model=%q err=%v points=%d",
				channel.Id, upstreamName, pErr, len(points)))
			// 单模型接口不可用时退回 summary 的 success_rate 单点，
			// 至少保留该模型最近窗口的汇总值。
			if m.SuccessRate >= 0 {
				out[alias] = []Point{{Ts: 0, Rate: m.SuccessRate}}
			}
			continue
		}
		out[alias] = points
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("upstream returned no model matching this channel")
	}
	return out, nil
}

// fetchModelSeries 读取单模型接口的逐小时 series。
func fetchModelSeries(ctx context.Context, channel *channelpkg.Channel, base, model string) ([]Point, error) {
	target := base + "/api/perf-metrics?model=" + url.QueryEscape(model) + "&hours=24"
	body, err := getWithChannelKey(ctx, channel, target)
	if err != nil {
		return nil, err
	}
	return parseModelSeries(body)
}

// parseModelSeries 解析单模型接口的逐小时序列。抽成纯函数以便单测：
// HTTP/SSRF 之外的解析逻辑必须能独立验证。
func parseModelSeries(body []byte) ([]Point, error) {
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
	points := make([]Point, 0, len(doc.Data.Series))
	for _, p := range doc.Data.Series {
		if p.Ts <= 0 || p.SuccessPct == nil {
			continue // 没有时间桶或成功率的点无法落到具体小时
		}
		point := Point{Ts: p.Ts, Rate: *p.SuccessPct}
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
