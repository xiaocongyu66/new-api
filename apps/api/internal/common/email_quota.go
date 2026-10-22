package common

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common/cachex"
	"github.com/samber/hot"
)

// The quota window is a calendar day in UTC+8, which matches both the
// operator's locale and the daily accounting Chinese mailbox providers use.
var smtpQuotaZone = time.FixedZone("CST", 8*60*60)

const (
	smtpQuotaCacheNamespace = "new-api:smtp_quota:v1"
	// A day's counter outlives its window by a few hours so a key created just
	// before midnight is never read again after the date rolls over.
	smtpQuotaTTL = 26 * time.Hour
)

var (
	smtpQuotaCacheOnce sync.Once
	smtpQuotaCache     *cachex.HybridCache[int]
)

func smtpDailyQuotaCache() *cachex.HybridCache[int] {
	smtpQuotaCacheOnce.Do(func() {
		smtpQuotaCache = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace(smtpQuotaCacheNamespace),
			Redis:     RDB,
			RedisEnabled: func() bool {
				return RedisEnabled && RDB != nil
			},
			RedisCodec: cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, 1024).
					WithTTL(smtpQuotaTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return smtpQuotaCache
}

func smtpQuotaKey(account string) string {
	return account + ":" + time.Now().In(smtpQuotaZone).Format("2006-01-02")
}

// smtpQuotaUsed reads how many sends the account already made today.
func smtpQuotaUsed(account SMTPAccountConfig) (int, error) {
	used, found, err := smtpDailyQuotaCache().Get(smtpQuotaKey(account.Account))
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return used, nil
}

// smtpAccountHasQuota reports whether the account may still send today. A cache
// read failure fails open: blocking every outbound mail over a counter hiccup
// is worse than a few extra sends, because the provider still enforces its own
// limit and the pool failover covers the rejection.
func smtpAccountHasQuota(account SMTPAccountConfig) bool {
	if account.DailyLimit <= 0 {
		return true
	}
	used, err := smtpQuotaUsed(account)
	if err != nil {
		SysError(fmt.Sprintf("failed to read SMTP quota for %s: %v", account.Account, err))
		return true
	}
	return used < account.DailyLimit
}

// smtpRecordQuotaUse charges one send against the account. Read-modify-write is
// good enough here: sends are low-concurrency and the quota is a safety
// threshold ahead of the provider's own limit, not an accounting boundary.
func smtpRecordQuotaUse(account SMTPAccountConfig) {
	if account.DailyLimit <= 0 {
		return
	}
	used, err := smtpQuotaUsed(account)
	if err != nil {
		SysError(fmt.Sprintf("failed to read SMTP quota for %s: %v", account.Account, err))
		return
	}
	if err := smtpDailyQuotaCache().SetWithTTL(smtpQuotaKey(account.Account), used+1, smtpQuotaTTL); err != nil {
		SysError(fmt.Sprintf("failed to record SMTP quota for %s: %v", account.Account, err))
	}
}

// smtpSendOrder picks the accounts eligible for one send: those whose daily
// limit is not yet reached, lowest priority tier first, rotated inside that
// tier so consecutive sends spread across the accounts of the same tier. A nil
// result means every account is out of quota.
func smtpSendOrder(accounts []SMTPAccountConfig) []SMTPAccountConfig {
	eligible := make([]SMTPAccountConfig, 0, len(accounts))
	for _, account := range accounts {
		if smtpAccountHasQuota(account) {
			eligible = append(eligible, account)
		}
	}
	if len(eligible) == 0 {
		return nil
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].Priority < eligible[j].Priority
	})

	topPriority := eligible[0].Priority
	tierEnd := 1
	for tierEnd < len(eligible) && eligible[tierEnd].Priority == topPriority {
		tierEnd++
	}

	tier := eligible[:tierEnd]
	if len(tier) > 1 {
		offset := int(smtpSendCursor.Add(1)) % len(tier)
		rotated := make([]SMTPAccountConfig, 0, len(tier))
		rotated = append(rotated, tier[offset:]...)
		rotated = append(rotated, tier[:offset]...)
		tier = rotated
	}

	ordered := make([]SMTPAccountConfig, 0, len(eligible))
	ordered = append(ordered, tier...)
	return append(ordered, eligible[tierEnd:]...)
}
