package identity

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserAuthFenceRollbackExpiresAndRecovers(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)

	user := User{
		Username:    "auth-fence-rollback",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, PopulateUserCache(user))

	tx := dbx.DB.Begin()
	require.NoError(t, tx.Error)
	next, err := IncrementUserAuthVersionWithTx(tx, user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, next)

	_, err = CacheGetUserBase(user.Id)
	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	cacheTTL, err := common.RDB.TTL(t.Context(), quotacache.UserKey(user.Id)).Result()
	require.NoError(t, err)
	fenceTTL, err := common.RDB.TTL(t.Context(), getUserAuthFenceKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Greater(t, fenceTTL, cacheTTL)
	require.NoError(t, tx.Rollback().Error)

	server.FastForward(time.Duration(userAuthFenceTTLSeconds()+1) * time.Second)
	assert.False(t, server.Exists(getUserAuthFenceKey(user.Id)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", committed)

	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, cached.AuthVersion)
}

func TestPendingUserAuthFenceRejectsStaleCacheWrite(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	const userID = 4201
	require.NoError(t, SetUserAuthVersionFence(userID, 2))

	err := writeUserCache(&UserBase{
		Id: userID, Group: "default", Username: "stale", AuthVersion: 1,
	}, true)

	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	assert.False(t, server.Exists(quotacache.UserKey(userID)))
}

func TestUserAuthFieldUpdateRejectsVersionMismatch(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	const userID = 4202
	require.NoError(t, writeUserCache(&UserBase{
		Id: userID, Group: "current", Username: "cached", AuthVersion: 3,
	}, true))

	err := updateUserCacheFieldAtVersion(userID, "Group", "stale", 2)

	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	group, err := common.RDB.HGet(t.Context(), quotacache.UserKey(userID), "Group").Result()
	require.NoError(t, err)
	assert.Equal(t, "current", group)
}

func TestRefreshUserGroupCacheRepairsDelayedSameVersionWrite(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username:    "delayed-group-refresh",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, PopulateUserCache(user))

	firstSnapshotRead := make(chan struct{})
	releaseDelayedRefresh := make(chan struct{})
	var intercepted atomic.Bool
	const callbackName = "test:block_delayed_group_refresh"
	require.NoError(t, dbx.DB.Callback().Query().After("gorm:query").Register(callbackName, func(*gorm.DB) {
		if intercepted.CompareAndSwap(false, true) {
			close(firstSnapshotRead)
			<-releaseDelayedRefresh
		}
	}))
	t.Cleanup(func() {
		_ = dbx.DB.Callback().Query().Remove(callbackName)
	})

	delayedResult := make(chan error, 1)
	go func() {
		delayedResult <- RefreshUserGroupCache(user.Id)
	}()
	<-firstSnapshotRead

	require.NoError(t, dbx.DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "pro").Error)
	require.NoError(t, RefreshUserGroupCache(user.Id))
	cached, err := CacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.EqualValues(t, 1, cached.AuthVersion)

	close(releaseDelayedRefresh)
	require.NoError(t, <-delayedResult)
	cached, err = CacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.EqualValues(t, 1, cached.AuthVersion)
}

func TestCommittedUserAuthVersionPermanentlyRejectsDelayedCacheFill(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)

	user := User{
		Username:    "auth-fence-commit",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, PopulateUserCache(user))
	stale := *user.ToBaseUser()

	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		_, err := IncrementUserAuthVersionWithTx(tx, user.Id)
		return err
	}))
	require.NoError(t, PublishUserAuthCache(user.Id))
	assert.False(t, server.Exists(getUserAuthFenceKey(user.Id)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)

	server.FastForward(time.Duration(userAuthFenceTTLSeconds()+1) * time.Second)
	require.NoError(t, common.RedisDelKey(quotacache.UserKey(user.Id)))
	err = writeUserCache(&stale, true)
	assert.True(t, errors.Is(err, ErrUserAuthCachePending))
	committed, err = common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)
}

func TestUserAuthVersionFenceAndCommittedFloorAreMonotonic(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)

	const userID = 4101
	require.NoError(t, SetUserAuthVersionFence(userID, 5))
	require.NoError(t, SetUserAuthVersionFence(userID, 3))
	pending, err := common.RDB.Get(t.Context(), getUserAuthFenceKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", pending)
	floor, err := getUserAuthVersionFloor(userID)
	require.NoError(t, err)
	assert.EqualValues(t, 5, floor)

	// Committing an older transaction must neither clear a newer pending fence
	// nor lower the effective deny floor.
	require.NoError(t, publishCommittedUserAuthVersion(userID, 3))
	pending, err = common.RDB.Get(t.Context(), getUserAuthFenceKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", pending)
	floor, err = getUserAuthVersionFloor(userID)
	require.NoError(t, err)
	assert.EqualValues(t, 5, floor)

	require.NoError(t, publishCommittedUserAuthVersion(userID, 5))
	assert.False(t, server.Exists(getUserAuthFenceKey(userID)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", committed)

	require.NoError(t, publishCommittedUserAuthVersion(userID, 4))
	committed, err = common.RDB.Get(t.Context(), getUserAuthVersionKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", committed)
}
