package billing

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func today() string { return time.Now().Format("2006-01-02") }
func now() int64    { return time.Now().Unix() }

// setupEvaluateTestDB opens an in-memory sqlite with both checkin tables so the
// cross-platform "already checked in today" queries have something to read.
func setupEvaluateTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Checkin{}, &QQCheckin{}))
	dbx.DB, dbx.LogDB = db, db
	return func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		_ = sqlDB.Close()
	}
}

// withCheckinSetting temporarily applies a checkin setting, restoring on cleanup.
func withCheckinSetting(t *testing.T, enabled, singlePlatform bool, min, max int) {
	t.Helper()
	s := GetCheckinSetting()
	orig := *s
	s.Enabled = enabled
	s.SinglePlatformOnly = singlePlatform
	s.MinQuota = min
	s.MaxQuota = max
	t.Cleanup(func() { *s = orig })
}

const (
	webUserId  = 1
	qqChannel  = true
	webChannel = false
)

func TestEvaluateDailyCheckinDisabled(t *testing.T) {
	defer setupEvaluateTestDB(t)()
	withCheckinSetting(t, false, true, 1000, 1000)

	_, err := evaluateDailyCheckin(webUserId, webChannel)
	require.Error(t, err)
	assert.Equal(t, "签到功能未启用", err.Error())
}

// TestEvaluateDailyCheckinSinglePlatformBothDirections is the regression that
// motivated the shared core: whichever channel checked in first must block the
// other one the same day.
func TestEvaluateDailyCheckinSinglePlatformBothDirections(t *testing.T) {
	defer setupEvaluateTestDB(t)()
	withCheckinSetting(t, true, true, 1000, 1000)

	// Web first: a web record must block the QQ channel too.
	require.NoError(t, dbx.DB.Create(&Checkin{UserId: webUserId, CheckinDate: today(), QuotaAwarded: 1000, CreatedAt: now()}).Error)
	_, err := evaluateDailyCheckin(webUserId, qqChannel)
	require.Error(t, err)
	assert.Equal(t, "今日已签到", err.Error())

	// QQ first on a different user: a QQ record must block the web channel too.
	require.NoError(t, dbx.DB.Create(&QQCheckin{UserId: 2, CheckinDate: today(), QuotaAwarded: 1000, CreatedAt: now()}).Error)
	_, err = evaluateDailyCheckin(2, webChannel)
	require.Error(t, err)
	assert.Equal(t, "今日已签到", err.Error())
}

// TestEvaluateDailyCheckinMultiPlatformIsolatesChannels: with single-platform
// off, a record on one channel must NOT block the other.
func TestEvaluateDailyCheckinMultiPlatformIsolatesChannels(t *testing.T) {
	defer setupEvaluateTestDB(t)()
	withCheckinSetting(t, true, false, 1000, 1000)

	require.NoError(t, dbx.DB.Create(&Checkin{UserId: webUserId, CheckinDate: today(), QuotaAwarded: 1000, CreatedAt: now()}).Error)
	quota, err := evaluateDailyCheckin(webUserId, qqChannel)
	require.NoError(t, err)
	assert.Equal(t, 1000, quota)
}

// TestEvaluateDailyCheckinQuotaRange covers the range math: the award stays in
// [min, max], and degenerate ranges clamp instead of producing a negative or
// nonsense award.
func TestEvaluateDailyCheckinQuotaRange(t *testing.T) {
	defer setupEvaluateTestDB(t)()

	cases := []struct {
		name    string
		min     int
		max     int
		wantMin int
		wantMax int
	}{
		{"normal range", 1000, 3000, 1000, 3000},
		{"equal bounds", 1000, 1000, 1000, 1000},
		{"negative min clamps to 0", -500, 1000, 0, 1000},
		{"inverted range collapses to min", 2000, 100, 2000, 2000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCheckinSetting(t, true, true, tc.min, tc.max)
			for i := 0; i < 50; i++ {
				quota, err := evaluateDailyCheckin(100+i, webChannel)
				require.NoError(t, err)
				assert.GreaterOrEqual(t, quota, tc.wantMin)
				assert.LessOrEqual(t, quota, tc.wantMax)
			}
		})
	}
}
