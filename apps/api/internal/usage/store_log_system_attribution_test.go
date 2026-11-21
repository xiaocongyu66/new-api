package usage

// 回归：自动探活 / 渠道测试是系统行为，不得计入任何用户自己的用量。
// 探活本身已改为只请求 /v1/models（零 token）；这里钉住剩下的日志口径：
// IsSystem 的消费日志必须落到系统名下（user_id=0、username=系统）且 quota=0，
// 否则运营者会在“我的用量”里看到一堆并非自己发起的测试消耗。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordConsumeLogSystemAttribution(t *testing.T) {
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousLogConsume, previousDataExport :=
		common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false

	require.NoError(t, db.AutoMigrate(&Log{}), "migrate logs table")
	require.NoError(t, db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, setting TEXT, deleted_at DATETIME)`).Error)

	t.Cleanup(func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled =
			previousRedis, previousLogConsume, previousDataExport
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	request := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	resp := testutil.ServeBufferedRoute(t, http.MethodPost, "/api/channel/test/1", []contract.Middleware{
		func(c contract.Context) {
			// The operator's own identity is on the context; a system log must
			// ignore it rather than bill the operator.
			c.Set("username", "operator")
			c.Next()
		},
	}, func(c contract.Context) {
		RecordConsumeLog(c, 42, RecordConsumeLogParams{
			ModelName: "test-model",
			TokenName: "模型测试",
			Quota:     12345,
			IsSystem:  true,
		})
		_ = c.JSON(http.StatusOK, common.H{"success": true})
	}, request)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var row Log
	require.NoError(t, db.Where("user_id = ?", 0).First(&row).Error, "system log must be owned by user 0")
	assert.Equal(t, "系统", row.Username)
	assert.Zero(t, row.Quota, "system logs must never carry billable quota")
	assert.Equal(t, "模型测试", row.TokenName)
}
