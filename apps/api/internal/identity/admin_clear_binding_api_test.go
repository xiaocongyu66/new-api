package identity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// performAdminClearBinding serves AdminClearUserBinding through a real Fiber
// route so :binding_type goes through URL parsing exactly as the admin binding
// dialog sends it. The provider-name-vs-column-name mismatch this guards
// against is invisible to a direct ClearBinding call.
func performAdminClearBinding(t *testing.T, userId int, bindingType string) (int, string) {
	t.Helper()
	target := "/api/user/" + strconv.Itoa(userId) + "/bindings/" + bindingType
	response := testutil.ServeBufferedRoute(t, http.MethodDelete, "/api/user/:id/bindings/:binding_type",
		[]contract.Middleware{func(c contract.Context) {
			c.Set("id", 9999)
			c.Set("role", common.RoleRootUser)
			c.Set("username", "root-operator")
			c.Next()
		}}, AdminClearUserBinding, httptest.NewRequest(http.MethodDelete, target, nil))
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode, string(payload)
}

// 绑定管理弹窗按 provider 名（github/qq/...）请求解绑。它一度发的是 users 表的
// 列名（github_id），除 email 外每个内置提供商都解不掉，且返回的是 200 +
// success:false，前端只弹一句 toast，看起来像"点了没反应"。
func TestAdminClearUserBindingAcceptsProviderNames(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	for _, tc := range []struct {
		bindingType string
		seed        func(user *User)
		stillSet    func(user *User) string
	}{
		{"github", func(u *User) { u.GitHubId = "gh-1" }, func(u *User) string { return u.GitHubId }},
		{"discord", func(u *User) { u.DiscordId = "dc-1" }, func(u *User) string { return u.DiscordId }},
		{"wechat", func(u *User) { u.WeChatId = "wx-1" }, func(u *User) string { return u.WeChatId }},
		{"oidc", func(u *User) { u.OidcId = "oidc-1" }, func(u *User) string { return u.OidcId }},
		{"linuxdo", func(u *User) { u.LinuxDOId = "ldo-1" }, func(u *User) string { return u.LinuxDOId }},
	} {
		t.Run(tc.bindingType, func(t *testing.T) {
			user := User{
				Username: "admin-clear-" + tc.bindingType, Password: "password",
				Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
				Group: "default", AffCode: "aff-" + tc.bindingType,
			}
			tc.seed(&user)
			require.NoError(t, dbx.DB.Create(&user).Error)

			status, body := performAdminClearBinding(t, user.Id, tc.bindingType)
			require.Equal(t, http.StatusOK, status)
			require.Contains(t, body, `"success":true`, "provider 名必须被后端接受")

			var stored User
			require.NoError(t, dbx.DB.First(&stored, user.Id).Error)
			assert.Empty(t, tc.stillSet(&stored))
		})
	}
}

// QQ 绑定不在 users 的列上，管理员解绑必须落到 qq_bindings，并释放 open_id 的
// 唯一索引，否则这个 QQ 号永久占位。
func TestAdminClearUserBindingClearsQQBinding(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)
	useUserCacheMiniRedis(t)

	const openID = "OPENIDADMINROUTE"
	user := User{
		Username: "admin-clear-qq", Password: "password",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "aff-qq-route",
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	seedQQBinding(t, user.Id, openID)

	status, body := performAdminClearBinding(t, user.Id, "qq")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"success":true`)

	bindings, codes := countQQRows(t, user.Id)
	assert.Zero(t, bindings)
	assert.Zero(t, codes)

	other := User{Username: "admin-clear-qq-next", Password: "password", AffCode: "aff-qq-next"}
	require.NoError(t, dbx.DB.Create(&other).Error)
	assert.NoError(t, dbx.DB.Create(&QQBinding{
		UserId: other.Id, OpenID: openID, CreatedAt: time.Now().Unix(),
	}).Error)
}

// 未知 binding_type 必须继续被拒，防止把任意字符串当列名去清空。
func TestAdminClearUserBindingRejectsUnknownType(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username: "admin-clear-unknown", Password: "password",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "aff-unknown",
	}
	require.NoError(t, dbx.DB.Create(&user).Error)

	_, body := performAdminClearBinding(t, user.Id, "github_id")
	assert.Contains(t, body, `"success":false`)
}
