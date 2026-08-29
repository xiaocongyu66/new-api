package channel_test

import (
	"github.com/QuantumNous/new-api/internal/billing"
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/task"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/internal/usage/record_perf"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	dbx.DB = db
	dbx.LogDB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	dbx.InitColumns()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&task.Task{},
		&identity.User{},
		&identity.UserSession{},
		&identity.AuthFlow{},
		&identity.ExternalIdentityClaim{},
		&identity.Token{},
		&identity.PasskeyCredential{},
		&identity.TwoFA{},
		&identity.TwoFABackupCode{},
		&usage.Log{},
		&catalog.Channel{},
		&usage.QuotaData{},
		&catalog.Ability{},
		&billing.TopUp{},
		&billing.SubscriptionPlan{},
		&billing.SubscriptionOrder{},
		&billing.UserSubscription{},
		&identity.UserOAuthBinding{},
		&record_perf.PerfMetric{},
		&ops.SystemInstance{},
		&ops.SystemTask{},
		&ops.SystemTaskLock{},
		&catalog.GatewayConfigRevision{},
		&catalog.GatewayConfigOutbox{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}
	if err := catalog.InitializeGatewayConfigRevision(); err != nil {
		panic("failed to initialize gateway revision: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		dbx.DB.Exec("DELETE FROM tasks")
		dbx.DB.Exec("DELETE FROM auth_flows")
		dbx.DB.Exec("DELETE FROM external_identity_claims")
		dbx.DB.Exec("DELETE FROM user_sessions")
		dbx.DB.Exec("DELETE FROM passkey_credentials")
		dbx.DB.Exec("DELETE FROM two_fa_backup_codes")
		dbx.DB.Exec("DELETE FROM two_fas")
		dbx.DB.Exec("DELETE FROM tokens")
		dbx.DB.Exec("DELETE FROM user_oauth_bindings")
		dbx.DB.Exec("DELETE FROM users")
		dbx.DB.Exec("DELETE FROM logs")
		dbx.DB.Exec("DELETE FROM channels")
		dbx.DB.Exec("DELETE FROM quota_data")
		dbx.DB.Exec("DELETE FROM abilities")
		dbx.DB.Exec("DELETE FROM top_ups")
		dbx.DB.Exec("DELETE FROM subscription_orders")
		dbx.DB.Exec("DELETE FROM subscription_plans")
		dbx.DB.Exec("DELETE FROM user_subscriptions")
		dbx.DB.Exec("DELETE FROM perf_metrics")
		dbx.DB.Exec("DELETE FROM system_instances")
		dbx.DB.Exec("DELETE FROM system_task_locks")
		dbx.DB.Exec("DELETE FROM system_tasks")
	})
}
