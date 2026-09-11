package usage

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
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

// 回归：全局封禁列表曾存在数据竞争——admin 后台保存
// user_insight_setting.blocked_clients 走 settings 反射直写，不持
// blockedClientsLock，与 relay 热路径上的 CheckClientBan 并发读撕裂
// slice header（-race 复现）。修复后所有写路径（含周期 option 同步）
// 都经 OnApplyUserInsightSetting 钩子在锁内应用。本测试必须带 -race 跑：
// 修复前 WARNING: DATA RACE，修复后干净。
func TestGlobalClientBanOptionPipelineNoRace(t *testing.T) {
	previousDB, previousRedis := dbx.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}))
	dbx.DB = db
	common.RedisEnabled = false
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
	})

	require.NoError(t, dbinfra.UpdateOption("user_insight_setting.blocked_clients", `["curl"]`))

	stop := make(chan struct{})
	var wg sync.WaitGroup
	// 写路径：模拟 admin 后台保存 / option 周期同步（settings 反射路径的入口）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = dbinfra.UpdateOption("user_insight_setting.blocked_clients", `["curl","okhttp"]`)
			}
		}
	}()
	// 读路径：relay 热路径形态。
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = CheckClientBan(1, "curl")
					_ = GetBlockedClientList()
				}
			}
		}()
	}
	for range 100 {
		_ = CheckClientBan(1, "curl")
	}
	close(stop)
	wg.Wait()
	// 并发窗口已过：同步写一次收尾值，验证 option 管线 + 钩子路径落内存。
	// （并发阶段 SQLite 写可能因锁竞争静默失败，收尾断言不能依赖它。）
	require.NoError(t, dbinfra.UpdateOption("user_insight_setting.blocked_clients", `["curl","okhttp"]`))
	assert.Equal(t, "global", CheckClientBan(1, "curl"))
	assert.ElementsMatch(t, []string{"curl", "okhttp"}, GetBlockedClientList())
}
