package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"time"
	identitymodel "github.com/QuantumNous/new-api/internal/identity/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// useUserCacheMiniRedis starts an in-memory Redis and points common.RDB at it.
// Values are restored on t.Cleanup.
func useUserCacheMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	oldSyncFrequency := common.SyncFrequency
	common.RedisEnabled = true
	common.SyncFrequency = 2
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
		common.SyncFrequency = oldSyncFrequency
	})
	return server
}

// populateUserCache hydrates the user cache hash for the given user.
func populateUserCache(user identitymodel.User) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetObj(identitymodel.GetUserCacheKey(user.Id), user.ToBaseUser(), time.Hour)
}

// cacheGetUserBase reads the cached user base.
func cacheGetUserBase(userID int) (*identitymodel.UserBase, error) {
	var ub identitymodel.UserBase
	err := common.RedisHGetObj(identitymodel.GetUserCacheKey(userID), &ub)
	return &ub, err
}

// require helpers for tests that need them
var _ = require.NoError
