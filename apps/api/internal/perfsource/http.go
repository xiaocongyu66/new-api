package perfsource

// 各级数据源共用的 HTTP 取数。所有出站请求都必须经过这里，以便统一
// SSRF 校验、超时与响应体上限——上游地址来自渠道配置，是不可信输入。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/egress"
)

const (
	upstreamTimeout = 15 * time.Second
	// 普通性能接口的响应上限。
	maxUpstreamBody = 8 << 20
	// /jk/status.json 是整站的监控快照，实测 13MB 量级，单独放宽。
	maxMonitorBody = 48 << 20
)

// newRequest 构造一个带 SSRF 校验与超时的上游 GET 请求。
func newRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	if err := egress.ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamTimeout)
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

// doRequest 发送请求并读取受限长度的响应体。
func doRequest(req *http.Request, limit int64) ([]byte, error) {
	// GetSSRFProtectedHTTPClient 在启动初始化前可能为 nil（例如后台任务先于
	// InitHttpClient 启动）。裸调用会 panic，这里兜底到一个带超时的客户端，
	// 初始 URL 已通过 ValidateSSRFProtectedFetchURL 校验。
	client := egress.GetSSRFProtectedHTTPClient()
	if client == nil {
		client = &http.Client{Timeout: upstreamTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("upstream has no such endpoint: %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// getWithChannelKey 用渠道自己的中转密钥请求上游。
func getWithChannelKey(ctx context.Context, channel *channelpkg.Channel, rawURL string) ([]byte, error) {
	return getWithChannelKeyLimit(ctx, channel, rawURL, maxUpstreamBody)
}

func getWithChannelKeyLimit(ctx context.Context, channel *channelpkg.Channel, rawURL string, limit int64) ([]byte, error) {
	req, err := newRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if key, _, keyErr := channel.GetNextEnabledKey(""); keyErr == nil && strings.TrimSpace(key) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	}
	return doRequest(req, limit)
}

// getWithBearer 用显式 bearer token 请求上游。
//
// sub2api 的 /api/v1/usage 走用户会话鉴权，中转密钥（sk-...）在那里无效，
// 所以必须允许传入渠道设置里单独配置的 token。
func getWithBearer(ctx context.Context, rawURL, token string) ([]byte, error) {
	req, err := newRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return doRequest(req, maxUpstreamBody)
}
