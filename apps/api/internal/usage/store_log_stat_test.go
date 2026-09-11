package usage

// Regression test for the usage-logs stat cards showing 总额度 = 0.
//
// SumUsedQuotaInternal / SumUsedQuota run two queries: the quota sum and the
// last-60s RPM/TPM rate. Both used to Scan into the SAME Stat struct, and the
// second Scan (columns rpm, tpm) reset Quota — a GORM struct scan assigns only
// the columns present in its result set, so /api/log/stat answered quota=0 for
// every date range while the log list itself showed the entries. The fix scans
// the rate query into its own variable; this test pins both fields surviving
// one call.

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogStatTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false

	t.Cleanup(func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.RedisEnabled = previousRedis
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedConsumeLogs(t *testing.T, db *gorm.DB, rows string, args ...any) {
	t.Helper()
	require.NoError(t, db.Exec(
		`CREATE TABLE logs (id INTEGER PRIMARY KEY, type INTEGER, quota INTEGER,
		 prompt_tokens INTEGER, completion_tokens INTEGER, created_at INTEGER)`).Error, "create logs table")
	require.NoError(t, db.Exec(`INSERT INTO logs (type, quota, prompt_tokens, completion_tokens, created_at) VALUES `+rows, args...).Error,
		"seed consume logs")
}

func TestSumUsedQuotaInternalKeepsQuotaAfterRateQuery(t *testing.T) {
	db := setupLogStatTestDB(t)

	now := time.Now().Unix()
	seedConsumeLogs(t, db,
		`(2, 5000, 100, 200, ?), (2, 4700, 300, 400, ?)`, now-5, now-10)

	stat, err := SumUsedQuotaInternal(2, now-60, now+60, "", "", "", 0, "")
	require.NoError(t, err)

	assert.Equal(t, 9700, stat.Quota,
		"the quota sum must survive the follow-up RPM/TPM query")
	assert.Equal(t, 2, stat.Rpm, "rate query must still populate RPM")
	assert.Equal(t, 1000, stat.Tpm, "rate query must still populate TPM")
}

func TestSumUsedQuotaKeepsQuotaAfterRateQuery(t *testing.T) {
	db := setupLogStatTestDB(t)

	now := time.Now().Unix()
	seedConsumeLogs(t, db, `(2, 3000, 10, 20, ?)`, now-3)

	stat, err := SumUsedQuota(2, now-60, now+60, "", "", "", 0, "")
	require.NoError(t, err)

	assert.Equal(t, 3000, stat.Quota,
		"the shared SumUsedQuota entry point must not lose the quota sum either")
	assert.Equal(t, 1, stat.Rpm)
}
