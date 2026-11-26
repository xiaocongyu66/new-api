package perfsource

// sub2api 凭证续期回归：
//   - 未过期 token 必须直接使用，不能发登录/刷新请求；
//   - 过期 token 必须走 refresh，而不是拿旧 token 硬用；
//   - refresh 失败后必须用账号密码登录；
//   - 登录返回非 0 code 必须报错；
//   - 任何失败路径的错误信息都不能回显凭据。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	channeldto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withSub2APIAuthDB 给凭证写回准备一个真实的渠道行：resolve 成功后会按
// channel.Id 重新读库并 Save，测试必须能从库里读到写回结果。
func withSub2APIAuthDB(t *testing.T, channel *channelpkg.Channel) {
	t.Helper()
	previous := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channelpkg.Channel{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })
	require.NoError(t, db.Create(channel).Error)
}

func loadSub2APIAuthSettings(t *testing.T, id int) channeldto.ChannelOtherSettings {
	t.Helper()
	var stored channelpkg.Channel
	require.NoError(t, dbx.DB.First(&stored, "id = ?", id).Error)
	return stored.GetOtherSettings()
}

func TestResolveSub2APITokenUsesUnexpiredToken(t *testing.T) {
	allowLoopback(t)

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 101, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-valid"
	settings.Sub2APITokenExpiresAt = time.Now().Add(time.Hour).Unix()
	channel.SetOtherSettings(settings)

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-valid", token)
	assert.Zero(t, atomic.LoadInt32(&requests), "未过期 token 不得触发 refresh/login 请求")
}

func TestResolveSub2APITokenRefreshesExpiredTokenAndPersists(t *testing.T) {
	allowLoopback(t)

	var refreshCalls, loginCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.RefreshToken != "rt-old" {
			http.Error(w, "unexpected refresh token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"at-new","refresh_token":"rt-new","expires_in":3600}}`))
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&loginCalls, 1)
		http.Error(w, "login must not be called", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 102, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-expired"
	settings.Sub2APITokenExpiresAt = time.Now().Add(-time.Minute).Unix()
	settings.Sub2APIRefreshToken = "rt-old"
	settings.Sub2APIUsername = "user@example.com"
	settings.Sub2APIPassword = "pw"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-new", token, "过期 token 必须换成 refresh 后的新 token")
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls))
	assert.Zero(t, atomic.LoadInt32(&loginCalls), "refresh 成功时不得再登录")

	stored := loadSub2APIAuthSettings(t, channel.Id)
	assert.Equal(t, "at-new", stored.Sub2APIAccessToken)
	assert.Equal(t, "rt-new", stored.Sub2APIRefreshToken, "轮换后的 refresh token 必须写回")
	assert.Greater(t, stored.Sub2APITokenExpiresAt, time.Now().Add(50*time.Minute).Unix())
	assert.Equal(t, "user@example.com", stored.Sub2APIUsername, "写回不得覆盖账号")
	assert.Equal(t, "pw", stored.Sub2APIPassword, "写回不得覆盖密码")
}

func TestResolveSub2APITokenFallsBackToLogin(t *testing.T) {
	allowLoopback(t)

	var refreshCalls, loginCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"refresh token expired"}`))
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&loginCalls, 1)
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Email != "user@example.com" || body.Password != "pw" {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"at-login","refresh_token":"rt-login","expires_in":7200}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 103, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-expired"
	settings.Sub2APITokenExpiresAt = time.Now().Add(-time.Minute).Unix()
	settings.Sub2APIRefreshToken = "rt-old"
	settings.Sub2APIUsername = "user@example.com"
	settings.Sub2APIPassword = "pw"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-login", token)
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls))
	assert.EqualValues(t, 1, atomic.LoadInt32(&loginCalls))

	stored := loadSub2APIAuthSettings(t, channel.Id)
	assert.Equal(t, "at-login", stored.Sub2APIAccessToken)
	assert.Equal(t, "rt-login", stored.Sub2APIRefreshToken)
	assert.Greater(t, stored.Sub2APITokenExpiresAt, time.Now().Add(100*time.Minute).Unix())
}

func TestResolveSub2APITokenWithoutCredentials(t *testing.T) {
	allowLoopback(t)

	channel := &channelpkg.Channel{Id: 104, BaseURL: &[]string{"http://127.0.0.1:1"}[0]}
	channel.SetOtherSettings(channeldto.ChannelOtherSettings{})

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "credentials not configured")
}

func TestResolveSub2APITokenReportsLoginCodeAndRedactsCredentials(t *testing.T) {
	allowLoopback(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"refresh token expired"}`))
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1001,"message":"invalid credentials"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 105, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-secret"
	settings.Sub2APITokenExpiresAt = time.Now().Add(-time.Minute).Unix()
	settings.Sub2APIRefreshToken = "rt-secret"
	settings.Sub2APIUsername = "user-secret@example.com"
	settings.Sub2APIPassword = "pw-secret"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.Error(t, err, "登录返回非 0 code 必须报错，不能当作成功")
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "code 1001")
	for _, secret := range []string{"at-secret", "rt-secret", "user-secret@example.com", "pw-secret"} {
		assert.NotContains(t, err.Error(), secret, "错误信息不得回显凭据")
	}
}

// TestResolveSub2APITokenRefreshesNearlyExpiredToken 钉住 60s 余量：
// 还剩 30s 的 token 也算过期，不能等真过期后让探活请求撞 401。
func TestResolveSub2APITokenRefreshesNearlyExpiredToken(t *testing.T) {
	allowLoopback(t)

	var refreshCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"at-fresh","refresh_token":"rt-fresh","expires_in":3600}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 106, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-nearly-expired"
	settings.Sub2APITokenExpiresAt = time.Now().Add(30 * time.Second).Unix()
	settings.Sub2APIRefreshToken = "rt-old"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	token, err := resolveSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-fresh", token)
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls))
}

// TestForceRefreshSub2APITokenIgnoresValidAccessToken 保证 401 重试路径不会
// 因为本地 token 看起来还没过期就再次拿旧值去撞上游。
func TestForceRefreshSub2APITokenIgnoresValidAccessToken(t *testing.T) {
	allowLoopback(t)

	var refreshCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"at-forced","refresh_token":"rt-forced","expires_in":3600}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{Id: 107, BaseURL: &[]string{srv.URL}[0]}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-still-valid"
	settings.Sub2APITokenExpiresAt = time.Now().Add(time.Hour).Unix()
	settings.Sub2APIRefreshToken = "rt-old"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	token, err := forceRefreshSub2APIToken(t.Context(), channel)
	require.NoError(t, err)
	assert.Equal(t, "at-forced", token)
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls))
}

// TestSub2APISourceRetriesOnceAfterUnauthorized 验证 401 后不直接失败：
// 作废旧 token → 强制 refresh → 用新 token 重新取数，且只重试一次。
func TestSub2APISourceRetriesOnceAfterUnauthorized(t *testing.T) {
	allowLoopback(t)

	var refreshCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-new" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"OK","data":{"items":[
			{"model":"m","duration_ms":1000,"first_token_ms":100,"created_at":"2026-11-22T10:05:00Z"}
		],"total":1,"page":1,"page_size":100,"pages":1}}`))
	})
	mux.HandleFunc("/api/v1/usage/errors", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"code":"OK","data":{"items":[],"total":0,"page":1,"page_size":100,"pages":1}}`))
	})
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"at-new","refresh_token":"rt-new","expires_in":3600}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	channel := &channelpkg.Channel{
		Id: 108, Type: 1, Key: "sk-relay", Name: "sub2api",
		Models: "m", BaseURL: &[]string{srv.URL}[0],
	}
	settings := channel.GetOtherSettings()
	settings.Sub2APIAccessToken = "at-old"
	// 过期时间未知：resolve 会直接使用 at-old，让上游用 401 拒绝它，
	// 从而真正走到「401 后作废 + 强制续期 + 重试一次」的路径。
	settings.Sub2APITokenExpiresAt = 0
	settings.Sub2APIRefreshToken = "rt-old"
	channel.SetOtherSettings(settings)
	withSub2APIAuthDB(t, channel)

	perf, err := (sub2APISource{}).Fetch(t.Context(), channel)
	require.NoError(t, err)
	points := perf["m"]
	require.Len(t, points, 1)
	assert.InDelta(t, 100, points[0].Rate, 0.001)
	assert.EqualValues(t, 1, atomic.LoadInt32(&refreshCalls), "401 后只允许续期一次")

	stored := loadSub2APIAuthSettings(t, channel.Id)
	assert.Equal(t, "at-new", stored.Sub2APIAccessToken)
}
