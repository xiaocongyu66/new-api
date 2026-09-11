package usage

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

// 锁定缓存过期契约：过期条目必须被重读，不能永远信缓存。
// 回归背景：TTL 曾误用 time.Second 的数值（纳秒量级）与秒级时间戳相加，
// 过期时间被推到天文数字，缓存永不失效——多实例部署时封禁变更
// 无法收敛到其他实例。
func TestGetUserClientBanSetExpiresStaleEntries(t *testing.T) {
	previousDB, previousRedis := dbx.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UserInsightClientBan{}))
	dbx.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
	})

	require.NoError(t, ToggleUserClientBan(7, "okhttp", true))

	// 塞入一条已过期的脏缓存：声称该用户封的是别的客户端。
	// 若过期逻辑失效，这次调用会返回脏数据而不是重读数据库。
	userClientBanCacheLock.Lock()
	userClientBanCache[7] = &userClientBanCacheEntry{
		set:       map[string]bool{"stale-client": true},
		expiresAt: common.GetTimestamp() - 1,
	}
	userClientBanCacheLock.Unlock()

	assert.Equal(t, map[string]bool{"okhttp": true}, GetUserClientBanSet(7))

	// TTL 必须是秒级数值：换算回时长应在分钟量级而不是纳秒。
	assert.Less(t, time.Duration(userClientBanCacheTTL)*time.Second, time.Minute)
}
