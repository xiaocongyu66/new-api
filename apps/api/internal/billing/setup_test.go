package billing

import (
	"os"
	"testing"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMain installs the process-wide database the billing tests charge against.
// It replaces the TestMain that used to live in settle_midjourney_test.go before
// midjourney settlement moved to internal/task; tests that never install their
// own database (webhook contract tests) still rely on this one.
func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	dbx.DB = db
	dbx.LogDB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	dbx.InitColumns()

	if err := db.AutoMigrate(
		&identity.User{},
		&identity.Token{},
		&usage.Log{},
		&usage.QuotaData{},
		&catalog.Channel{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&ops.SystemTask{},
		&ops.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}
