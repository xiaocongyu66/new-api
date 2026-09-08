package billing_test

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// enableCheckin turns on the checkin feature for a test, restoring on cleanup.
func enableCheckin(t *testing.T) {
	setting := billing.GetCheckinSetting()
	origEnabled := setting.Enabled
	origMin := setting.MinQuota
	origMax := setting.MaxQuota
	setting.Enabled = true
	setting.MinQuota = 1000
	setting.MaxQuota = 1000
	t.Cleanup(func() {
		setting.Enabled = origEnabled
		setting.MinQuota = origMin
		setting.MaxQuota = origMax
	})
}

// setupCheckinTestDB opens an in-memory sqlite and migrates the User + QQBinding
// tables. Returns a cleanup func the caller should defer.
func setupCheckinTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB, previousRedis := dbx.DB, dbx.LogDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&identity.User{}, &identity.QQBinding{}, &billing.Checkin{}, &billing.CheckinRecord{}))
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	return func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	}
}

func TestDoCheckinRequiresQQBound(t *testing.T) {
	defer setupCheckinTestDB(t)()
	enableCheckin(t)

	// 创建未绑定 QQ 的用户
	user := &identity.User{Username: "noqq", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)

	// 打开 RequireQQBound
	setting := billing.GetCheckinSetting()
	orig := setting.RequireQQBound
	setting.RequireQQBound = true
	t.Cleanup(func() { setting.RequireQQBound = orig })

	c, rec := fiberadapter.NewSyntheticContext(nil)
	c.Set("id", user.Id)
	billing.DoCheckin(c)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "qq_not_bound")
	assert.Contains(t, rec.Body.String(), "请先绑定 QQ")
}

func TestDoCheckinSucceedsWhenQQBound(t *testing.T) {
	defer setupCheckinTestDB(t)()
	enableCheckin(t)

	user := &identity.User{Username: "qqbound", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, dbx.DB.Create(
		&identity.QQBinding{UserId: user.Id, OpenID: "OPENID_TEST"},
	).Error)

	setting := billing.GetCheckinSetting()
	orig := setting.RequireQQBound
	setting.RequireQQBound = true
	t.Cleanup(func() { setting.RequireQQBound = orig })

	c, rec := fiberadapter.NewSyntheticContext(nil)
	c.Set("id", user.Id)
	billing.DoCheckin(c)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "签到成功")
}

func TestDoCheckinNoRequireWhenDisabled(t *testing.T) {
	defer setupCheckinTestDB(t)()
	enableCheckin(t)

	user := &identity.User{Username: "free", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, dbx.DB.Create(user).Error)

	// RequireQQBound 默认 false
	setting := billing.GetCheckinSetting()
	orig := setting.RequireQQBound
	setting.RequireQQBound = false
	t.Cleanup(func() { setting.RequireQQBound = orig })

	c, rec := fiberadapter.NewSyntheticContext(nil)
	c.Set("id", user.Id)
	billing.DoCheckin(c)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "签到成功")
}
