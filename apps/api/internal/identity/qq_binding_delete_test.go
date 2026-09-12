package identity

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateQQBindingTables 单独迁移 QQ 相关表。
//
// TestMain 的 AutoMigrate 名单里没有这两张表，直接在用到的测试里迁移，
// 避免为了一个回归用例去改动全包共享的 TestMain。AutoMigrate 幂等。
func migrateQQBindingTables(t *testing.T) {
	t.Helper()
	require.NoError(t, dbx.DB.AutoMigrate(&QQBinding{}, &QQBindCode{}))
	t.Cleanup(func() {
		dbx.DB.Exec("DELETE FROM qq_bindings")
		dbx.DB.Exec("DELETE FROM qq_bind_codes")
	})
}

func seedQQBinding(t *testing.T, userId int, openID string) {
	t.Helper()
	require.NoError(t, dbx.DB.Create(&QQBinding{
		UserId: userId, OpenID: openID, Username: "tester",
		CreatedAt: time.Now().Unix(),
	}).Error)
	require.NoError(t, dbx.DB.Create(&QQBindCode{
		Code: openID[:6], UserId: userId,
		ExpiredAt: time.Now().Add(QQBindCodeTTL).Unix(),
		CreatedAt: time.Now().Unix(),
	}).Error)
}

func countQQRows(t *testing.T, userId int) (bindings int64, codes int64) {
	t.Helper()
	require.NoError(t, dbx.DB.Model(&QQBinding{}).Where("user_id = ?", userId).Count(&bindings).Error)
	require.NoError(t, dbx.DB.Model(&QQBindCode{}).Where("user_id = ?", userId).Count(&codes).Error)
	return
}

// 线上事故回归：管理员硬删除用户后 qq_bindings 残留，open_id 唯一索引把这个
// QQ 号永久占死，用户换新账号后再也绑不上。
func TestHardDeleteUserUnbindsQQ(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)
	useUserCacheMiniRedis(t)

	const openID = "OPENIDHARDDELETE"
	user := User{Username: "qq-hard-delete", Password: "password", AuthVersion: 1, AffCode: "affhard1"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	seedQQBinding(t, user.Id, openID)

	require.NoError(t, HardDeleteUserById(user.Id))

	bindings, codes := countQQRows(t, user.Id)
	assert.Zero(t, bindings, "硬删除后不应残留 QQ 绑定")
	assert.Zero(t, codes, "硬删除后不应残留未使用的绑定验证码")

	// 关键断言：open_id 已释放，新账号能绑定同一个 QQ。
	newUser := User{Username: "qq-hard-delete-new", Password: "password", AffCode: "affhard2"}
	require.NoError(t, dbx.DB.Create(&newUser).Error)
	assert.NoError(t, dbx.DB.Create(&QQBinding{
		UserId: newUser.Id, OpenID: openID, CreatedAt: time.Now().Unix(),
	}).Error)
}

// 软删除（用户自助注销 DeleteSelf）同样必须解绑，否则 QQ 机器人会继续
// 按旧绑定给一个已注销的 user_id 发额度。
func TestSoftDeleteUserUnbindsQQ(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)
	useUserCacheMiniRedis(t)

	const openID = "OPENIDSOFTDELETE"
	user := User{Username: "qq-soft-delete", Password: "password", AuthVersion: 1, AffCode: "affsoft1"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	seedQQBinding(t, user.Id, openID)

	require.NoError(t, DeleteUserById(user.Id))

	var alive int64
	require.NoError(t, dbx.DB.Model(&User{}).Where("id = ?", user.Id).Count(&alive).Error)
	assert.Zero(t, alive, "软删除后用户不应可见")

	bindings, codes := countQQRows(t, user.Id)
	assert.Zero(t, bindings, "软删除后不应残留 QQ 绑定")
	assert.Zero(t, codes, "软删除后不应残留未使用的绑定验证码")

	// 软删除保留 users 行，aff_code 的唯一索引仍生效，因此新账号必须换一个。
	newUser := User{Username: "qq-soft-delete-new", Password: "password", AffCode: "affsoft2"}
	require.NoError(t, dbx.DB.Create(&newUser).Error)
	assert.NoError(t, dbx.DB.Create(&QQBinding{
		UserId: newUser.Id, OpenID: openID, CreatedAt: time.Now().Unix(),
	}).Error)
}

// 解绑失败必须让整个删除事务回滚，不能出现"用户没了、绑定还在"的中间态。
func TestDeleteQQBindingWithTxRejectsZeroId(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)

	assert.Error(t, DeleteQQBindingWithTx(dbx.DB, 0))
}

// 管理员清除绑定走 ClearBinding，QQ 不在 users 的列上，必须落到 qq_bindings；
// 未使用的验证码也要一起作废，否则用户拿旧码在群里发一次就绑回来了。
func TestClearBindingQQRemovesBindingAndPendingCodes(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)

	const openID = "OPENIDADMINCLEAR"
	user := User{Username: "qq-admin-clear", Password: "password", AffCode: "affclear1"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	seedQQBinding(t, user.Id, openID)

	require.NoError(t, user.ClearBinding("qq"))

	bindings, codes := countQQRows(t, user.Id)
	assert.Zero(t, bindings, "清除绑定后不应残留 QQ 绑定")
	assert.Zero(t, codes, "清除绑定后不应残留未使用的绑定验证码")

	// open_id 已释放，同一个 QQ 能绑到别的账号。
	other := User{Username: "qq-admin-clear-new", Password: "password", AffCode: "affclear2"}
	require.NoError(t, dbx.DB.Create(&other).Error)
	assert.NoError(t, dbx.DB.Create(&QQBinding{
		UserId: other.Id, OpenID: openID, CreatedAt: time.Now().Unix(),
	}).Error)
}

// 未绑定 QQ 的用户被清除 QQ 绑定不应报错：绑定管理弹窗按 qq_open_id 判断是否
// 展示解绑按钮，而这个字段只有管理端单用户接口才返回。
func TestClearBindingQQOnUnboundUserSucceeds(t *testing.T) {
	truncateTables(t)
	migrateQQBindingTables(t)

	user := User{Username: "qq-never-bound", Password: "password", AffCode: "affunbound"}
	require.NoError(t, dbx.DB.Create(&user).Error)

	assert.NoError(t, user.ClearBinding("qq"))
}
