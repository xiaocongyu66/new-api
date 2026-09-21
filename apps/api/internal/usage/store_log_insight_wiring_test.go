package usage

// 回归：Fiber 移植（#488）时 RecordConsumeLog 丢失了 attachInsight 调用，
// 画像管道（other.insight / 复核样本 / 用户画像聚合）在生产静默停摆两周，
// 线上 67k 条消费日志无一携带 insight 字段。本测试钉住接线：
// 上下文里的画像结果必须进入消费日志的 other，且原有字段不丢。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/QuantumNous/new-api/internal/usage/insight"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordConsumeLogAttachesInsight(t *testing.T) {
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

	setting := GetUserInsightSetting()
	previousEnabled, previousSample, previousRecordInLog :=
		setting.Enabled, setting.SampleEnabled, setting.RecordInLog
	setting.Enabled, setting.RecordInLog, setting.SampleEnabled = true, true, false

	require.NoError(t, db.AutoMigrate(&Log{}), "migrate logs table")
	// GetUserSetting 只读 id + setting 两列，最小建表即可（deleted_at 为 User 软删列）。
	require.NoError(t, db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, setting TEXT, deleted_at DATETIME)`).Error)

	t.Cleanup(func() {
		setting.Enabled, setting.SampleEnabled, setting.RecordInLog =
			previousEnabled, previousSample, previousRecordInLog
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled =
			previousRedis, previousLogConsume, previousDataExport
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := testutil.ServeBufferedRoute(t, http.MethodPost, "/v1/chat/completions", []contract.Middleware{
		func(c contract.Context) {
			c.Set("username", "insight-wiring")
			c.Set(string(constant.ContextKeyUserInsight), &insight.Result{Client: "test_cli", ClientName: "Test CLI"})
			c.Next()
		},
	}, func(c contract.Context) {
		RecordConsumeLog(c, 42, RecordConsumeLogParams{
			ModelName: "test-model",
			Other:     common.H{"billing_source": "wallet"},
		})
		_ = c.JSON(http.StatusOK, common.H{"success": true})
	}, request)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var row Log
	require.NoError(t, db.Where("user_id = ?", 42).First(&row).Error, "consume log must be written")
	assert.Equal(t, "insight-wiring", row.Username)
	assert.Contains(t, row.Other, `"insight"`, "attachInsight must merge the insight result into other")
	assert.Contains(t, row.Other, "billing_source", "pre-existing other fields must survive")
}
