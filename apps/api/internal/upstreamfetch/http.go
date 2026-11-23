package upstreamfetch

// 上游取数共用的 HTTP 层：SSRF 校验、超时、响应体上限、鉴权头。
// 所有出站请求都必须经过这里——上游地址来自渠道配置，是不可信输入。

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
	DefaultTimeout = 15 * time.Second
	// 普通接口的响应上限。
	defaultBodyLimit = 8 << 20
	// 监控类快照可能是十几 MB 的整站数据。
	LargeBodyLimit = 48 << 20
)

// Get 发一次带 SSRF 校验与超时的 GET，返回受长度限制的响应体。
func Get(ctx context.Context, rawURL string, timeout time.Duration, limit int64, apply func(*http.Request)) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if limit <= 0 {
		limit = defaultBodyLimit
	}
	if err := egress.ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	if apply != nil {
		apply(req)
	}

	// GetSSRFProtectedHTTPClient 在启动初始化前可能为 nil（后台任务先于
	// InitHttpClient 启动）。裸调用会 panic，这里兜底到带超时的客户端，
	// 初始 URL 已通过 ValidateSSRFProtectedFetchURL 校验。
	client := egress.GetSSRFProtectedHTTPClient()
	if client == nil {
		client = &http.Client{Timeout: timeout}
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

// ApplyAuth 按 Spec 的鉴权方式给请求加上凭证。
func ApplyAuth(req *http.Request, channel *channelpkg.Channel, auth Auth) {
	switch auth.Mode {
	case AuthChannelKey:
		if channel == nil {
			return
		}
		if key, _, err := channel.GetNextEnabledKey(""); err == nil && strings.TrimSpace(key) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
		}
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(auth.Token))
	case AuthHeader:
		req.Header.Set(strings.TrimSpace(auth.Header), strings.TrimSpace(auth.Token))
	case AuthNone:
	}
}
