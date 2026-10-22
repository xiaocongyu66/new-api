package mailer

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/cachex"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/samber/hot"
	"gorm.io/gorm"
)

// The quota window is a calendar day in UTC+8, which matches both the
// operator's locale and the daily accounting Chinese mailbox providers use.
var quotaZone = time.FixedZone("CST", 8*60*60)

const (
	quotaCacheNamespace = "new-api:smtp_quota:v1"
	// A day's counter outlives its window by a few hours so a key created just
	// before midnight is never read again after the date rolls over.
	quotaTTL = 26 * time.Hour
)

// UsedToday reads the account's send count for today. The cache is only a
// read-through accelerator: the database row is the source of truth, so a
// restart replays the count instead of resetting it.
func UsedToday(account string) (int, error) {
	if used, found, err := quotaCache().Get(quotaKey(account)); err == nil && found {
		return used, nil
	}
	if dbx.DB == nil {
		return 0, nil
	}

	var record SMTPQuotaUsage
	err := dbx.DB.Where("account = ? AND day = ?", account, quotaDay()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}

	_ = quotaCache().SetWithTTL(quotaKey(account), record.Used, quotaTTL)
	return record.Used, nil
}

// RecordUse charges one successful send against the account. The increment is a
// single atomic UPDATE rather than an ON CONFLICT upsert: the expression form
// ports to SQLite, MySQL and PostgreSQL unchanged, while upsert assignment
// syntax differs between them.
func RecordUse(account string) error {
	if dbx.DB == nil {
		return nil
	}
	if err := increment(account); err != nil {
		return err
	}
	// Drop the cached count so the next read observes the database row.
	_, _ = quotaCache().DeleteMany([]string{quotaKey(account)})
	return nil
}

func increment(account string) error {
	day := quotaDay()
	now := time.Now()

	result := dbx.DB.Model(&SMTPQuotaUsage{}).
		Where("account = ? AND day = ?", account, day).
		Updates(map[string]interface{}{"used": gorm.Expr("used + 1"), "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// First send of the day for this account.
	err := dbx.DB.Create(&SMTPQuotaUsage{Account: account, Day: day, Used: 1, UpdatedAt: now}).Error
	if err == nil {
		return nil
	}
	// A concurrent send inserted the row between the UPDATE and the INSERT.
	return dbx.DB.Model(&SMTPQuotaUsage{}).
		Where("account = ? AND day = ?", account, day).
		Updates(map[string]interface{}{"used": gorm.Expr("used + 1"), "updated_at": now}).Error
}

// PurgeCache drops every cached counter. Tests use it to prove that a count
// survives the cache being lost, which is what a restart looks like.
func PurgeCache() error {
	return quotaCache().Purge()
}

func quotaDay() string {
	return time.Now().In(quotaZone).Format("2006-01-02")
}

func quotaKey(account string) string {
	return account + ":" + quotaDay()
}

var (
	quotaCacheOnce sync.Once
	quotaCacheInst *cachex.HybridCache[int]
)

// Wire installs this package as the quota store behind the SMTP account pool.
// The database bootstrap calls it once the handles are ready.
func Wire() {
	common.SetSMTPQuotaStore(store{})
}

// store implements common.SMTPQuotaStore.
type store struct{}

func (store) UsedToday(account string) (int, error) { return UsedToday(account) }
func (store) RecordUse(account string) error        { return RecordUse(account) }

func quotaCache() *cachex.HybridCache[int] {
	quotaCacheOnce.Do(func() {
		quotaCacheInst = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace(quotaCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, 1024).
					WithTTL(quotaTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return quotaCacheInst
}
