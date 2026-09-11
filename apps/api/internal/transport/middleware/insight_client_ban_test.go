package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withClientBanEnv 在内存 sqlite 上准备 options 表与封禁表，
// 并通过真实 option 管线（dbinfra.UpdateOption → settings.ApplyOption）
// 开启总开关、写入全局封禁名单——与生产写路径完全一致，
// 避免测试直接改 usage 包私有变量而绕过配置链路。
func withClientBanEnv(t *testing.T) {
	t.Helper()
	previousDB, previousRedis := dbx.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}, &usage.UserInsightClientBan{}))
	dbx.DB = db
	common.RedisEnabled = false
	// ApplyOption 会回写 OptionMap，测试进程里它可能还未初始化。
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	// 默认开启总开关（走真实 option 管线）；要测"开关关闭"的用例再显式关。
	require.NoError(t, dbinfra.UpdateOption("user_insight_setting.client_ban_enabled", "true"))
	t.Cleanup(func() {
		// 先把进程内配置复位，再恢复数据库句柄：顺序反了会把 true 留在
		// 内存里，污染后续不涉及封禁的测试。
		_ = dbinfra.UpdateOption("user_insight_setting.client_ban_enabled", "false")
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
	})
}

func withGlobalBan(t *testing.T, clients ...string) {
	t.Helper()
	for _, client := range clients {
		require.NoError(t, usage.SetGlobalClientBan(client, true))
	}
	t.Cleanup(func() {
		for _, client := range clients {
			_ = usage.SetGlobalClientBan(client, false)
		}
	})
}

func performClientBanRequest(t *testing.T, userAgent string, userID int) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	middlewares := []contract.Middleware{}
	if userID > 0 {
		userID := userID
		middlewares = append(middlewares, func(c contract.Context) {
			c.Set("id", userID)
			c.Next()
		})
	}
	middlewares = append(middlewares, InsightClientBan())
	return testutil.ServeBufferedRoute(t, http.MethodPost, "/v1/chat/completions", middlewares, func(c contract.Context) {
		_ = c.JSON(http.StatusOK, common.H{"success": true})
	}, request)
}

// 总开关关闭时必须全放行：这是运营方的紧急停用通道，
// 任何"开关关了还拦请求"的回归都会直接切断站点流量。
func TestInsightClientBanDisabledPassesEverything(t *testing.T) {
	withClientBanEnv(t)
	require.NoError(t, dbinfra.UpdateOption("user_insight_setting.client_ban_enabled", "false"))
	require.NoError(t, usage.SetGlobalClientBan("curl", true))

	require.Equal(t, http.StatusOK, performClientBanRequest(t, "curl/8.5.0", 0).StatusCode)
}

func TestInsightClientBanRejectsGloballyBlockedClient(t *testing.T) {
	withClientBanEnv(t)
	withGlobalBan(t, "curl", "ua:deepseek-harness")

	require.Equal(t, http.StatusForbidden, performClientBanRequest(t, "curl/8.5.0", 0).StatusCode)
	// 自动发现标识与内置规则走同一条判定路径，都能被拦。
	require.Equal(t, http.StatusForbidden, performClientBanRequest(t, "deepseek-harness/0.1.1-rc.2", 0).StatusCode)
	// 名单外的客户端不受影响。
	require.Equal(t, http.StatusOK, performClientBanRequest(t, "RikkaHub-Android/2.4.5", 0).StatusCode)
}

func TestInsightClientBanPerUserScope(t *testing.T) {
	withClientBanEnv(t)
	// 裸 okhttp UA（无专属头）识别为 "okhttp"；带专属头才会细分出
	// ua:okhttp+x-... 标识。封谁就得拦谁，这里封裸标识。
	require.NoError(t, usage.ToggleUserClientBan(7, "okhttp", true))

	// 单用户档只拦目标用户：同一个客户端，其他用户照常通过。
	require.Equal(t, http.StatusForbidden, performClientBanRequest(t, "okhttp/4.12.0", 7).StatusCode)
	require.Equal(t, http.StatusOK, performClientBanRequest(t, "okhttp/4.12.0", 8).StatusCode)

	// 解除后立即恢复（缓存失效路径）。
	require.NoError(t, usage.ToggleUserClientBan(7, "okhttp", false))
	require.Equal(t, http.StatusOK, performClientBanRequest(t, "okhttp/4.12.0", 7).StatusCode)
}

// 无 UA / 无法识别的请求没有任何标识可比对，必须放行：
// 封禁名单匹配的是识别结果，识别不出就不能动。
func TestInsightClientBanPassesUnidentifiableRequests(t *testing.T) {
	withClientBanEnv(t)
	withGlobalBan(t, "curl")

	require.Equal(t, http.StatusOK, performClientBanRequest(t, "", 0).StatusCode)
}
