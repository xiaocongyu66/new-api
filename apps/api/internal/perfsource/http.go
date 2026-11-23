package perfsource

// 各级数据源共用的 HTTP 取数。所有出站请求都必须经过这里，以便统一
// SSRF 校验、超时与响应体上限——上游地址来自渠道配置，是不可信输入。
//
// 真正的传输实现放在 internal/upstreamfetch，价格同步与探活共用同一份，
// 避免两套出站逻辑各自演化出不同的安全边界。

import (
	"context"
	"net/http"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/upstreamfetch"
)

const (
	upstreamTimeout = upstreamfetch.DefaultTimeout
	// 普通性能接口的响应上限。
	maxUpstreamBody = 8 << 20
	// /jk/status.json 是整站的监控快照，实测 13MB 量级，单独放宽。
	maxMonitorBody = upstreamfetch.LargeBodyLimit
)

// getWithChannelKey 用渠道自己的中转密钥请求上游。
func getWithChannelKey(ctx context.Context, channel *channelpkg.Channel, rawURL string) ([]byte, error) {
	return getWithChannelKeyLimit(ctx, channel, rawURL, maxUpstreamBody)
}

func getWithChannelKeyLimit(ctx context.Context, channel *channelpkg.Channel, rawURL string, limit int64) ([]byte, error) {
	return upstreamfetch.Get(ctx, rawURL, upstreamTimeout, limit, func(req *http.Request) {
		if key, _, keyErr := channel.GetNextEnabledKey(""); keyErr == nil && strings.TrimSpace(key) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
		}
	})
}

// getWithBearer 用显式 bearer token 请求上游。
//
// sub2api 的 /api/v1/usage 走用户会话鉴权，中转密钥（sk-...）在那里无效，
// 所以必须允许传入渠道设置里单独配置的 token。
func getWithBearer(ctx context.Context, rawURL, token string) ([]byte, error) {
	return upstreamfetch.Get(ctx, rawURL, upstreamTimeout, maxUpstreamBody, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	})
}
