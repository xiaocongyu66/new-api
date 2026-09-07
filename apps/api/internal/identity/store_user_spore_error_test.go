package identity

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupSporeErrorTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	previousRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&User{}))
	dbx.DB = db
	dbx.LogDB = db
	common.RedisEnabled = false
	// Stub audit hooks to avoid real LogDB insert
	RegisterAuditHooks(func(int, string) {}, nil, nil, nil)
	t.Cleanup(func() {
		dbx.DB = previousDB
		dbx.LogDB = previousLogDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	})
}

// TestSporeInsufficientErrorMessage 验证错误信息携带具体余额和所需数量。
func TestSporeInsufficientErrorMessage(t *testing.T) {
	err := &SporeInsufficientError{Current: 15, Required: 20}
	msg := err.Error()
	assert.Contains(t, msg, "菌种余额不足")
	assert.Contains(t, msg, "1.5")
	assert.Contains(t, msg, "2.0")
	assert.True(t, errors.Is(err, ErrSporeInsufficient), "must unwrap to sentinel")
}

// TestDecreaseUserSporeReturnsDetailedError 验证 DecreaseUserSporeTx 返回带金额的错误。
func TestDecreaseUserSporeReturnsDetailedError(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "spore-err-test",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	// Top up to 1.5 spore (15 units)
	require.NoError(t, IncreaseUserSpore(user.Id, 15))

	// Try to deduct 2.0 spore (20 units) — should fail with detailed error
	err := DecreaseUserSpore(user.Id, 20)
	require.Error(t, err)
	var sporeErr *SporeInsufficientError
	require.True(t, errors.As(err, &sporeErr))
	assert.EqualValues(t, 15, sporeErr.Current)
	assert.EqualValues(t, 20, sporeErr.Required)

	// Balance must remain untouched on failure
	bal, _ := GetUserSpore(user.Id)
	assert.EqualValues(t, 15, bal)
}

// TestAdminAdjustUserSporeAtomicOnFailure 验证 AdminAdjustUserSpore 是原子的。
func TestAdminAdjustUserSporeAtomicOnFailure(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "atomic-test",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, IncreaseUserSpore(user.Id, 15))

	// Subtract more than available — should fail atomically
	err := AdminAdjustUserSpore(user.Id, "subtract", 20)
	assert.Error(t, err)
	var sporeErr *SporeInsufficientError
	require.True(t, errors.As(err, &sporeErr))

	// Balance must not have changed
	bal, _ := GetUserSpore(user.Id)
	assert.EqualValues(t, 15, bal, "subtract must roll back on insufficient")
}

// TestAdminAdjustUserSporeInvalidMode 验证未知模式返回错误，余额不变。
func TestAdminAdjustUserSporeInvalidMode(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "invalid-mode",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, IncreaseUserSpore(user.Id, 50))

	err := AdminAdjustUserSpore(user.Id, "unknown_mode", 5)
	assert.Error(t, err)
	bal, _ := GetUserSpore(user.Id)
	assert.EqualValues(t, 50, bal)
}

// TestAdminAdjustUserSporeOverrideRejectsNegative 验证 override 模式拒绝负数。
func TestAdminAdjustUserSporeOverrideRejectsNegative(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "neg-test",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, IncreaseUserSpore(user.Id, 50))

	err := AdminAdjustUserSpore(user.Id, "override", -5)
	assert.Error(t, err)
	bal, _ := GetUserSpore(user.Id)
	assert.EqualValues(t, 50, bal)
}

// TestDecreaseUserSporeRejectsZeroAndNegative 验证 DecreaseUserSpore 拒绝 0 和负数。
func TestDecreaseUserSporeRejectsZeroAndNegative(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "zero-neg",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, IncreaseUserSpore(user.Id, 50))

	assert.Error(t, DecreaseUserSpore(user.Id, 0))
	assert.Error(t, DecreaseUserSpore(user.Id, -1))
}

// TestDecreaseUserSporeTxZeroNoop 验证 DecreaseUserSporeTx 对 0 是 no-op。
func TestDecreaseUserSporeTxZeroNoop(t *testing.T) {
	setupSporeErrorTestDB(t)
	user := &User{
		Username: "tx-zero",
		Password: "x",
		Role:     1,
		Status:   1,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	require.NoError(t, IncreaseUserSpore(user.Id, 50))

	// Calling with 0 should be a no-op (returns nil)
	require.NoError(t, DecreaseUserSporeTx(dbx.DB, user.Id, 0))
	bal, _ := GetUserSpore(user.Id)
	assert.EqualValues(t, 50, bal, "zero decrease must not change balance")
}
