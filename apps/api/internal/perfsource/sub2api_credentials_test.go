package perfsource

// sub2api 凭据来源的回归：
//   - 渠道 settings 有凭据时，账号表里的同名 host 不得抢走优先级；
//   - 渠道没有凭据时，按 host 复用账号表里的账号；
//   - 两边都没有时必须报“未配置”，不能静默用空 token 打上游；
//   - 续期得到的新 token 只写回它来自的那一份，不污染另一侧；
//   - HTTP 响应序列化后不得出现 password/access_token/refresh_token 的值。

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withUpstreamAccountDB(t *testing.T) *gorm.DB {
	t.Helper()
	previous := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channelpkg.UpstreamAccount{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })
	return db
}

func TestSub2APICredentialsPreferChannelSettings(t *testing.T) {
	db := withUpstreamAccountDB(t)
	require.NoError(t, db.Create(&channelpkg.UpstreamAccount{
		Host:         "channel.example.com",
		BaseURL:      "https://channel.example.com",
		Kind:         channelpkg.UpstreamAccountKindSub2API,
		Username:     "account-user",
		Password:     "account-pass",
		AccessToken:  "account-token",
		RefreshToken: "account-refresh",
		Enabled:      true,
	}).Error)

	channel := &channelpkg.Channel{Id: 1, BaseURL: &[]string{"https://channel.example.com/"}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIUsername = "channel-user"
	settings.Sub2APIPassword = "channel-pass"
	settings.Sub2APIAccessToken = "channel-token"
	settings.Sub2APIRefreshToken = "channel-refresh"
	settings.Sub2APITokenExpiresAt = 1700000000
	channel.SetOtherSettings(settings)

	credentials, err := sub2apiCredentials(channel)
	require.NoError(t, err)
	assert.Equal(t, sub2apiCredentialSourceChannel, credentials.Source)
	assert.Equal(t, "https://channel.example.com", credentials.BaseURL)
	assert.Equal(t, "channel-user", credentials.Username)
	assert.Equal(t, "channel-pass", credentials.Password)
	assert.Equal(t, "channel-token", credentials.AccessToken)
	assert.Equal(t, "channel-refresh", credentials.RefreshToken)
	assert.EqualValues(t, 1700000000, credentials.TokenExpiresAt)

	// 解析只读：解析前后的渠道设置必须逐字段相同。
	assert.Equal(t, settings, channel.GetOtherSettings(), "解析凭据不得改写渠道设置")
}

func TestSub2APICredentialsFallBackToUpstreamAccount(t *testing.T) {
	db := withUpstreamAccountDB(t)
	require.NoError(t, db.Create(&channelpkg.UpstreamAccount{
		Host:           "fallback.example.com",
		BaseURL:        "https://fallback.example.com",
		Kind:           channelpkg.UpstreamAccountKindSub2API,
		Username:       "account-user",
		Password:       "account-pass",
		AccessToken:    "account-token",
		RefreshToken:   "account-refresh",
		TokenExpiresAt: 1700000001,
		Enabled:        true,
	}).Error)

	channel := &channelpkg.Channel{Id: 2, BaseURL: &[]string{"https://fallback.example.com/some/path"}[0]}

	credentials, err := sub2apiCredentials(channel)
	require.NoError(t, err)
	assert.Equal(t, sub2apiCredentialSourceAccount, credentials.Source)
	assert.Equal(t, "https://fallback.example.com", credentials.BaseURL)
	assert.Equal(t, "account-user", credentials.Username)
	assert.Equal(t, "account-pass", credentials.Password)
	assert.Equal(t, "account-token", credentials.AccessToken)
	assert.Equal(t, "account-refresh", credentials.RefreshToken)
	assert.EqualValues(t, 1700000001, credentials.TokenExpiresAt)
	assert.NotZero(t, credentials.AccountId)
}

func TestSub2APICredentialsErrorWhenNeitherSideHasCredentials(t *testing.T) {
	withUpstreamAccountDB(t)
	channel := &channelpkg.Channel{Id: 3, BaseURL: &[]string{"https://empty.example.com"}[0]}

	credentials, err := sub2apiCredentials(channel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials not configured")
	assert.Equal(t, sub2apiCredentialSourceNone, credentials.Source)
}

func TestResolveSub2APITokenPersistsRenewedTokenOnAccount(t *testing.T) {
	allowLoopback(t)

	var refreshCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"at-renewed","refresh_token":"rt-renewed","expires_in":3600}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	host, err := channelpkg.NormalizeUpstreamAccountHost(server.URL)
	require.NoError(t, err)
	db := withUpstreamAccountDB(t)
	account := &channelpkg.UpstreamAccount{
		Host:         host,
		BaseURL:      server.URL,
		Kind:         channelpkg.UpstreamAccountKindSub2API,
		RefreshToken: "rt-old",
		Enabled:      true,
	}
	require.NoError(t, db.Create(account).Error)

	channel := &channelpkg.Channel{Id: 4, BaseURL: &[]string{server.URL}[0]}
	token, err := resolveSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-renewed", token)
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls))

	var stored channelpkg.UpstreamAccount
	require.NoError(t, db.First(&stored, "id = ?", account.Id).Error)
	assert.Equal(t, "at-renewed", stored.AccessToken)
	assert.Equal(t, "rt-renewed", stored.RefreshToken)
	assert.Greater(t, stored.TokenExpiresAt, int64(0))
	assert.Empty(t, channel.GetOtherSettings().Sub2APIAccessToken, "账号续期不得写回渠道 settings")
}

func TestUpstreamAccountResponseDoesNotExposeSecrets(t *testing.T) {
	account := &channelpkg.UpstreamAccount{
		Id:           1,
		Host:         "secret.example.com",
		BaseURL:      "https://secret.example.com",
		Kind:         channelpkg.UpstreamAccountKindSub2API,
		Username:     "demo@example.com",
		Password:     "pw-secret-value",
		AccessToken:  "at-secret-value",
		RefreshToken: "rt-secret-value",
		Enabled:      true,
	}

	payload, err := common.Marshal(account.ToResponse())
	require.NoError(t, err)
	text := string(payload)
	for _, secret := range []string{"pw-secret-value", "at-secret-value", "rt-secret-value"} {
		assert.NotContains(t, text, secret, "响应不得回显凭据值")
	}
	for _, field := range []string{`"password"`, `"access_token"`, `"refresh_token"`} {
		assert.NotContains(t, text, field, "响应不得包含凭据字段")
	}
	assert.Contains(t, text, `"has_password":true`)
	assert.Contains(t, text, `"has_token":true`)

	// 模型本身也可能被别的 handler 直接序列化；json:"-" 必须同样兜住。
	rawAccount, err := common.Marshal(account)
	require.NoError(t, err)
	for _, secret := range []string{"pw-secret-value", "at-secret-value", "rt-secret-value"} {
		assert.NotContains(t, string(rawAccount), secret, "模型序列化不得回显凭据值")
	}
}

// TestProbeSub2APIAccountSummarizesKeysAndModels 钉住测试连接依赖的上游形状：
// 密钥分页里的 group_id 既可能是数字也可能是字符串，模型广场按组给模型。
func TestProbeSub2APIAccountSummarizesKeysAndModels(t *testing.T) {
	allowLoopback(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"at-probe","refresh_token":"rt-probe","expires_in":3600}}`))
	})
	mux.HandleFunc("/api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[
			{"id":1,"group_id":99},
			{"id":2,"group_id":"99"},
			{"id":3,"group_id":7}
		],"total":3,"page":1,"page_size":50,"pages":1}}`))
	})
	mux.HandleFunc("/api/v1/model-plaza", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"groups":[
			{"id":99,"name":"g99","models":[{"name":"model-a"},{"name":"model-b"}]},
			{"id":7,"name":"g7","models":[{"name":"model-c"}]},
			{"id":8,"name":"g8","models":[{"name":"model-d"}]}
		]}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	probe, err := ProbeSub2APIAccount(t.Context(), server.URL, "user@example.com", "pw", "")
	require.NoError(t, err)
	assert.Equal(t, 3, probe.KeyCount)
	assert.Equal(t, []string{"7", "99"}, probe.GroupIDs)
	assert.True(t, probe.ModelCountAvailable)
	assert.Equal(t, 3, probe.ModelCount, "只统计账号分组 7 与 99 下的模型")
}
