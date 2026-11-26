package perfsource

// sub2api 面板的用户会话凭证续期。
//
// sub2api 的 /api/v1/usage 与 /api/v1/usage/errors 只认用户 access token
// （sk-... 中转密钥会被判 INVALID_TOKEN）。access token 有效期约 24h：
// 过期后先用 refresh_token 轮换；refresh 也失效时再用面板账号密码登录。
// 两条路都返回同一个 {code,data:{access_token,refresh_token,expires_in}}。

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	// sub2apiTokenExpiryMargin 让即将过期的 token 提前换新，避免探活请求
	// 正好撞上过期瞬间。
	sub2apiTokenExpiryMargin = 60 * time.Second
	sub2apiAuthBodyLimit     = 1 << 20
)

// sub2apiAuthToken 是登录/刷新接口 data 部分的统一形状。
type sub2apiAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type sub2apiAuthEnvelope struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    sub2apiAuthToken `json:"data"`
}

// resolveSub2APIToken 按回退链拿一个可用的 bearer token：
//  1. 已配置且未过期的 access token
//  2. 用 refresh_token 换新 token
//  3. 用 username + password 重新登录
//
// 凭据可能来自渠道 settings，也可能来自按 host 复用的上游账号；续期后的
// 新 token 只写回它来自的那一处。
func resolveSub2APIToken(ctx context.Context, channel *channelpkg.Channel) (string, error) {
	credentials, err := sub2apiCredentials(channel)
	if err != nil {
		return "", err
	}
	if credentials.AccessToken != "" && !sub2apiTokenExpired(credentials.TokenExpiresAt, time.Now()) {
		return credentials.AccessToken, nil
	}
	return refreshSub2APIToken(ctx, channel, credentials)
}

// forceRefreshSub2APIToken 跳过第 1 步，直接走 2→3。
// 供上游返回 401 时主动作废本地 token 后重试。
func forceRefreshSub2APIToken(ctx context.Context, channel *channelpkg.Channel) (string, error) {
	credentials, err := sub2apiCredentials(channel)
	if err != nil {
		return "", err
	}
	return refreshSub2APIToken(ctx, channel, credentials)
}

// sub2apiCanRenew 判断这套凭据在 token 作废后还有没有办法换新的。
func sub2apiCanRenew(credentials sub2apiCredentialSet) bool {
	if strings.TrimSpace(credentials.RefreshToken) != "" {
		return true
	}
	return strings.TrimSpace(credentials.Username) != "" && credentials.Password != ""
}

// sub2apiTokenExpired 按 expires_at > 0 && now >= expires_at - 60s 判定过期；
// expires_at == 0 表示未知，直接尝试使用。
func sub2apiTokenExpired(expiresAt int64, now time.Time) bool {
	if expiresAt <= 0 {
		return false
	}
	return !now.Before(time.Unix(expiresAt, 0).Add(-sub2apiTokenExpiryMargin))
}

func refreshSub2APIToken(ctx context.Context, channel *channelpkg.Channel, credentials sub2apiCredentialSet) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(credentials.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("channel has no base url")
	}

	var failures []string
	if refreshToken := strings.TrimSpace(credentials.RefreshToken); refreshToken != "" {
		token, err := sub2apiRefreshToken(ctx, base, refreshToken)
		if err == nil {
			persistSub2APICredentials(channel, credentials, token)
			return token.AccessToken, nil
		}
		failures = append(failures, "refresh: "+err.Error())
	}

	username := strings.TrimSpace(credentials.Username)
	password := credentials.Password
	if username != "" && password != "" {
		token, err := sub2apiLogin(ctx, base, username, password)
		if err == nil {
			persistSub2APICredentials(channel, credentials, token)
			return token.AccessToken, nil
		}
		failures = append(failures, "login: "+err.Error())
	}

	if len(failures) == 0 {
		return "", fmt.Errorf("sub2api credentials not configured: need an access token, refresh token, or username/password")
	}
	return "", fmt.Errorf("sub2api could not obtain a token: %s", strings.Join(failures, "; "))
}

func sub2apiRefreshToken(ctx context.Context, base, refreshToken string) (*sub2apiAuthToken, error) {
	body, err := sub2apiPostJSON(ctx, base+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	})
	if err != nil {
		return nil, err
	}
	return parseSub2APIAuthResponse(body, "refresh")
}

func sub2apiLogin(ctx context.Context, base, username, password string) (*sub2apiAuthToken, error) {
	// sub2api 的登录请求把账号放在 email 字段。
	body, err := sub2apiPostJSON(ctx, base+"/api/v1/auth/login", map[string]string{
		"email":    username,
		"password": password,
	})
	if err != nil {
		return nil, err
	}
	return parseSub2APIAuthResponse(body, "login")
}

func parseSub2APIAuthResponse(body []byte, action string) (*sub2apiAuthToken, error) {
	var envelope sub2apiAuthEnvelope
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("unparseable sub2api %s response: %w", action, err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("sub2api %s returned code %d", action, envelope.Code)
	}
	if strings.TrimSpace(envelope.Data.AccessToken) == "" {
		return nil, fmt.Errorf("sub2api %s returned no access token", action)
	}
	return &envelope.Data, nil
}

// sub2apiPostJSON 通过共享的 SSRF 防护客户端向 sub2api 发 JSON POST。
// 上游地址来自渠道配置，是不可信输入，不能绕过 egress 的校验。
func sub2apiPostJSON(ctx context.Context, rawURL string, payload any) ([]byte, error) {
	if err := egress.ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := egress.GetSSRFProtectedHTTPClient()
	if client == nil {
		client = &http.Client{Timeout: upstreamTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// 把「凭据不对」和「上游自己挂了」分开报：两者都表现为登录失败，但
		// 处理方式完全不同——401/403 要改密码，5xx/502 只能等上游恢复。
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("sub2api rejected the credentials (HTTP %d): check the account and password", resp.StatusCode)
		case resp.StatusCode >= 500:
			return nil, fmt.Errorf("sub2api is unavailable right now (HTTP %d): the upstream itself is failing, retry later", resp.StatusCode)
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, fmt.Errorf("sub2api rate limited the login (HTTP 429): retry later")
		default:
			return nil, fmt.Errorf("sub2api auth failed with HTTP %d", resp.StatusCode)
		}
	}
	return io.ReadAll(io.LimitReader(resp.Body, sub2apiAuthBodyLimit))
}

func persistSub2APICredentials(channel *channelpkg.Channel, credentials sub2apiCredentialSet, token *sub2apiAuthToken) {
	if token == nil {
		return
	}
	if credentials.Source == sub2apiCredentialSourceAccount {
		accessToken := strings.TrimSpace(token.AccessToken)
		if accessToken == "" {
			return
		}
		if err := channelpkg.SaveUpstreamAccountToken(credentials.AccountId, accessToken, token.RefreshToken, sub2apiTokenExpiresAt(token)); err != nil {
			common.SysLog(fmt.Sprintf("sub2api account token persist failed: account_id=%d: %v", credentials.AccountId, err))
		}
		return
	}
	persistSub2APIToken(channel, token)
}

func persistSub2APIToken(channel *channelpkg.Channel, token *sub2apiAuthToken) {
	if token == nil {
		return
	}
	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" {
		return
	}
	expiresAt := sub2apiTokenExpiresAt(token)
	updateSub2APISettings(channel, func(settings *dto.ChannelOtherSettings) {
		settings.Sub2APIAccessToken = accessToken
		if refreshToken := strings.TrimSpace(token.RefreshToken); refreshToken != "" {
			settings.Sub2APIRefreshToken = refreshToken
		}
		settings.Sub2APITokenExpiresAt = expiresAt
	})
}

func sub2apiTokenExpiresAt(token *sub2apiAuthToken) int64 {
	if token == nil || token.ExpiresIn <= 0 {
		return 0
	}
	return time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
}

// invalidateSub2APIToken 只清掉失效的 access token 与过期时间，
// 保留 username/password/refresh_token 供 forceRefreshSub2APIToken 续期。
// 清哪一份取决于凭据来自哪里：渠道与账号各存各的 token，不能互相污染。
func invalidateSub2APIToken(channel *channelpkg.Channel) {
	credentials, err := sub2apiCredentials(channel)
	if err != nil {
		return
	}
	// 只有存在续期路径时才把作废持久化。渠道只配了一个 access token 时，
	// 清库就等于把唯一凭据销毁：一次瞬时 401（上游重启、WAF 误判）之后，
	// 下一轮会退化成“没配置凭据”，运维连“鉴权被拒”都看不到。
	if !sub2apiCanRenew(credentials) {
		return
	}
	if credentials.Source == sub2apiCredentialSourceAccount {
		if err := channelpkg.ClearUpstreamAccountAccessToken(credentials.AccountId); err != nil {
			common.SysLog(fmt.Sprintf("sub2api account token invalidate failed: account_id=%d: %v", credentials.AccountId, err))
		}
		return
	}
	updateSub2APISettings(channel, func(settings *dto.ChannelOtherSettings) {
		settings.Sub2APIAccessToken = ""
		settings.Sub2APITokenExpiresAt = 0
	})
}

// updateSub2APISettings 用 channel.Id 重新读取最新 settings 再合并修改，
// 避免拿旧快照覆盖别人刚改过的配置。保存失败只记日志，不影响本次取数。
func updateSub2APISettings(channel *channelpkg.Channel, mutate func(*dto.ChannelOtherSettings)) {
	if channel == nil || mutate == nil {
		return
	}
	target := channel
	persist := false
	if channel.Id > 0 && dbx.DB != nil {
		var latest channelpkg.Channel
		if err := dbx.DB.First(&latest, "id = ?", channel.Id).Error; err != nil {
			// 读不到最新行时不 Save，避免用旧快照覆盖别人的修改；但本次
			// 取数仍用内存里的新 token，不能因为落库失败而失败。
			common.SysLog(fmt.Sprintf("sub2api settings persist skipped: channel_id=%d reload failed: %v", channel.Id, err))
		} else {
			target = &latest
			persist = true
		}
	}
	settings := target.GetOtherSettings()
	mutate(&settings)
	target.SetOtherSettings(settings)
	if target != channel {
		channel.OtherSettings = target.OtherSettings
	}

	if !persist || target.Id == 0 {
		return
	}
	if err := target.Save(); err != nil {
		common.SysLog(fmt.Sprintf("sub2api settings persist failed: channel_id=%d: %v", channel.Id, err))
	}
}
