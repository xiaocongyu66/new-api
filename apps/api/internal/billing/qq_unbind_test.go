package billing_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupQQBindTestDB migrates the tables the QQ bind handlers touch, including
// qq_bind_codes, which the checkin fixture does not need.
func setupQQBindTestDB(t *testing.T) {
	t.Helper()
	cleanup := setupCheckinTestDB(t)
	require.NoError(t, dbx.DB.AutoMigrate(&identity.QQBindCode{}))
	t.Cleanup(cleanup)
}

func enableQQCheckin(t *testing.T) {
	t.Helper()
	setting := billing.GetQQBotSetting()
	original := setting.QQCheckinEnabled
	setting.QQCheckinEnabled = true
	t.Cleanup(func() { setting.QQCheckinEnabled = original })
}

// 自助解绑：绑定状态必须真的翻回未绑定，前端卡片正是按这个字段决定显示解绑
// 按钮还是验证码生成入口。
func TestUnbindQQFlipsBindStatusToUnbound(t *testing.T) {
	setupQQBindTestDB(t)
	enableQQCheckin(t)

	user := &identity.User{Username: "qq-self-unbind", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, dbx.DB.Create(&identity.QQBinding{
		UserId: user.Id, OpenID: "OPENID_SELF_UNBIND", Username: "tester",
		CreatedAt: time.Now().Unix(),
	}).Error)

	boundCtx, boundRec := fiberadapter.NewSyntheticContext(nil)
	boundCtx.Set("id", user.Id)
	billing.GetQQBindStatus(boundCtx)
	require.Equal(t, http.StatusOK, boundRec.Code)
	require.Contains(t, boundRec.Body.String(), `"bound":true`)

	unbindCtx, unbindRec := fiberadapter.NewSyntheticContext(nil)
	unbindCtx.Set("id", user.Id)
	billing.UnbindQQ(unbindCtx)
	require.Equal(t, http.StatusOK, unbindRec.Code)
	assert.Contains(t, unbindRec.Body.String(), `"success":true`)

	afterCtx, afterRec := fiberadapter.NewSyntheticContext(nil)
	afterCtx.Set("id", user.Id)
	billing.GetQQBindStatus(afterCtx)
	require.Equal(t, http.StatusOK, afterRec.Code)
	assert.Contains(t, afterRec.Body.String(), `"bound":false`)
}

// 解绑后必须能重新生成验证码。CreateQQBindCode 在用户仍有绑定行时直接报错，
// 所以这条断言同时证明绑定行真的被删掉了，而不只是状态接口读不到。
func TestUnbindQQAllowsGeneratingANewBindCode(t *testing.T) {
	setupQQBindTestDB(t)
	enableQQCheckin(t)

	user := &identity.User{Username: "qq-rebind", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, dbx.DB.Create(&identity.QQBinding{
		UserId: user.Id, OpenID: "OPENID_REBIND", CreatedAt: time.Now().Unix(),
	}).Error)

	blockedCtx, blockedRec := fiberadapter.NewSyntheticContext(nil)
	blockedCtx.Set("id", user.Id)
	billing.GenerateQQBindCode(blockedCtx)
	require.Contains(t, blockedRec.Body.String(), `"success":false`)

	unbindCtx, _ := fiberadapter.NewSyntheticContext(nil)
	unbindCtx.Set("id", user.Id)
	billing.UnbindQQ(unbindCtx)

	regeneratedCtx, regeneratedRec := fiberadapter.NewSyntheticContext(nil)
	regeneratedCtx.Set("id", user.Id)
	billing.GenerateQQBindCode(regeneratedCtx)
	require.Equal(t, http.StatusOK, regeneratedRec.Code)
	assert.Contains(t, regeneratedRec.Body.String(), `"success":true`)
}

// 解绑要连该用户的验证码一起清掉，和注销账号的行为保持一致。
//
// 这里显式造出「已绑定 + 存在一个未使用且未过期的验证码」这个状态：正常流程
// 下 ConsumeQQBindCode 会把用掉的码标记为 used，但那次 UPDATE 的错误是被丢掉
// 的（qq_binding.go 内），失败后就会留下这样一行。绑定还在时它兑换不了
// （ConsumeQQBindCode 会拒绝已绑定的账号），可一旦解绑，这个码在 TTL 内又变成
// 了可用的绑定凭证。
func TestUnbindQQRemovesPendingBindCode(t *testing.T) {
	setupQQBindTestDB(t)
	enableQQCheckin(t)

	user := &identity.User{Username: "qq-stale-code", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, dbx.DB.Create(&identity.QQBinding{
		UserId: user.Id, OpenID: "OPENID_STALE", CreatedAt: time.Now().Unix(),
	}).Error)
	pending := identity.QQBindCode{
		Code: "#StaLe1", UserId: user.Id, Used: false,
		ExpiredAt: time.Now().Add(identity.QQBindCodeTTL).Unix(),
		CreatedAt: time.Now().Unix(),
	}
	require.NoError(t, dbx.DB.Create(&pending).Error)

	unbindCtx, unbindRec := fiberadapter.NewSyntheticContext(nil)
	unbindCtx.Set("id", user.Id)
	billing.UnbindQQ(unbindCtx)
	require.Equal(t, http.StatusOK, unbindRec.Code)
	require.Contains(t, unbindRec.Body.String(), `"success":true`)

	var remaining int64
	require.NoError(t, dbx.DB.Model(&identity.QQBindCode{}).Where("user_id = ?", user.Id).Count(&remaining).Error)
	assert.Zero(t, remaining, "解绑后不应残留绑定验证码")

	// 关键断言：那个码已经不能再把账号绑回来。
	_, err := identity.ConsumeQQBindCode(pending.Code, "OPENID_OTHER", "", "someone-else")
	assert.Error(t, err)
}
