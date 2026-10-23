package mailer

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupQuotaTestDB points the package database at an in-memory SQLite instance
// with the quota table migrated, so quota counting can be exercised without the
// production database.
func setupQuotaTestDB(t *testing.T) {
	t.Helper()

	previousDB := dbx.DB
	t.Cleanup(func() { dbx.DB = previousDB })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SMTPQuotaUsage{}))
	dbx.DB = db

	// A fresh database must not inherit the previous test's cached counts.
	require.NoError(t, PurgeCache())
}

func TestQuotaSurvivesCacheLoss(t *testing.T) {
	setupQuotaTestDB(t)

	const account = "a@example.com"
	require.NoError(t, RecordUse(account))

	// Dropping the cache simulates a restart: the count must come back from the
	// database instead of resetting to zero.
	require.NoError(t, PurgeCache())

	used, err := UsedToday(account)
	require.NoError(t, err)
	require.Equal(t, 1, used)
}

func TestQuotaIncrementsPerSend(t *testing.T) {
	setupQuotaTestDB(t)

	const account = "a@example.com"
	for i := 0; i < 3; i++ {
		require.NoError(t, RecordUse(account))
	}

	require.NoError(t, PurgeCache())
	used, err := UsedToday(account)
	require.NoError(t, err)
	require.Equal(t, 3, used)
}

func TestQuotaIsScopedToAccountAndDay(t *testing.T) {
	setupQuotaTestDB(t)

	require.NoError(t, RecordUse("a@example.com"))

	used, err := UsedToday("b@example.com")
	require.NoError(t, err)
	require.Equal(t, 0, used)

	var rows []SMTPQuotaUsage
	require.NoError(t, dbx.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "a@example.com", rows[0].Account)
	require.Equal(t, quotaDay(), rows[0].Day)
}

func TestQuotaReadWithoutDatabaseReturnsZero(t *testing.T) {
	setupQuotaTestDB(t)

	previousDB := dbx.DB
	dbx.DB = nil
	t.Cleanup(func() { dbx.DB = previousDB })
	require.NoError(t, PurgeCache())

	used, err := UsedToday("a@example.com")
	require.NoError(t, err)
	require.Equal(t, 0, used)
}

func TestQuotaDayIsAZeroPaddedDate(t *testing.T) {
	day := quotaDay()
	require.Len(t, day, len("2006-01-02"))
	_, err := time.Parse("2006-01-02", day)
	require.NoError(t, err)
}

func TestMemoryCacheIsUsableWithoutRedis(t *testing.T) {
	setupQuotaTestDB(t)

	// No Redis configured: the counter must still work through the in-memory
	// cache, which is the production path on hosts without Redis.
	require.NoError(t, RecordUse("a@example.com"))
	used, err := UsedToday("a@example.com")
	require.NoError(t, err)
	require.Equal(t, 1, used)
}
