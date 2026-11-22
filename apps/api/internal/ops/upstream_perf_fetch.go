package ops

// 上游性能取数：探活不再读取本地日志（那是探活自己的回声），而是调用上游站
// 暴露的性能接口（new-api 的 /api/perf-metrics/*、sub2api 的 /api/v1/usage），
// 拿上游站自己统计的模型成功率与延迟。零推理消耗，数据来自上游真实流量。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/egress"
)

const upstreamPerfTimeout = 15 * time.Second

// upstreamPerfSnapshot 是一次上游取数的结果：该渠道下每个模型的
// 成功率与平均延迟（毫秒）。
type upstreamPerfSnapshot struct {
	Models map[string]UpstreamPerfEntry
}

// UpstreamPerfEntry 是一个模型在上游站的性能观测。
type UpstreamPerfEntry struct {
	SuccessRate float64 // 0-100
	AvgLatency  int64   // ms；上游未给出时为 0（无证据）
}

// FetchUpstreamPerf 调用上游站的性能接口，返回模型名 → 性能条目。
// 上游没有该接口（404）、鉴权失败或返回体不可解析时返回错误，调用方据此
// 跳过该渠道，而不是捏造数据。
func FetchUpstreamPerf(ctx context.Context, channel *channelpkg.Channel) (*upstreamPerfSnapshot, error) {
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	url := base + "/api/perf-metrics/summary?hours=24"

	ctx, cancel := context.WithTimeout(ctx, upstreamPerfTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key, _, keyErr := channel.GetNextEnabledKey(""); keyErr == nil && strings.TrimSpace(key) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	}

	if err := egress.ValidateSSRFProtectedFetchURL(url); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}
	client := &http.Client{Timeout: upstreamPerfTimeout}
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	// new-api 形态：{"success":true,"data":{"models":[{model_name,success_rate,avg_latency_ms}]}}
	var doc struct {
		Success bool `json:"success"`
		Data    struct {
			Models []struct {
				ModelName   string  `json:"model_name"`
				SuccessRate float64 `json:"success_rate"`
				AvgLatency  *int64  `json:"avg_latency_ms"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("unparseable perf body: %w", err)
	}
	out := &upstreamPerfSnapshot{Models: make(map[string]UpstreamPerfEntry, len(doc.Data.Models))}
	for _, m := range doc.Data.Models {
		name := strings.TrimSpace(m.ModelName)
		if name == "" {
			continue
		}
		entry := UpstreamPerfEntry{SuccessRate: m.SuccessRate}
		if m.AvgLatency != nil && *m.AvgLatency > 0 {
			entry.AvgLatency = *m.AvgLatency
		}
		out.Models[name] = entry
	}
	return out, nil
}
