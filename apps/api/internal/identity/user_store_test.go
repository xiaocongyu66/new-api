package identity

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserStoreTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousRedis := dbx.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	dbx.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
	})
}

func insertUsersForPaginationTest(t *testing.T, total int) {
	t.Helper()
	for id := 1; id <= total; id++ {
		user := &model.User{
			Id:          id,
			Username:    fmt.Sprintf("user%02d", id),
			Password:    "password123",
			DisplayName: fmt.Sprintf("User %02d", id),
			Email:       fmt.Sprintf("user%02d@example.com", id),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AffCode:     fmt.Sprintf("aff%02d", id),
		}
		require.NoError(t, dbx.DB.Create(user).Error)
	}
}

func createUserBindTestUser(t *testing.T) model.User {
	t.Helper()
	user := model.User{
		Username:    "bind-test-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "bind-test-aff-code",
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	return user
}

func collectUserIDs(users []*model.User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	return ids
}

func TestGetAllUsersSortsBeforePagination(t *testing.T) {
	setupUserStoreTestDB(t)
	insertUsersForPaginationTest(t, 42)

	pageOne, total, err := listAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, model.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, collectUserIDs(pageOne))

	pageTwo, total, err := listAllUsers(&common.PageInfo{Page: 2, PageSize: 20}, model.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(pageTwo))

	pageThree, total, err := listAllUsers(&common.PageInfo{Page: 3, PageSize: 20}, model.NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{41, 42}, collectUserIDs(pageThree))
}

func TestUpdateUserAccessTokenOnlyUpdatesAccessToken(t *testing.T) {
	setupUserStoreTestDB(t)

	user := model.User{
		Id:              2,
		Username:        "token-rotation-user",
		Password:        "password",
		DisplayName:     "before",
		Status:          common.UserStatusEnabled,
		Quota:           1000,
		AffQuota:        800,
		AffHistoryQuota: 1200,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)

	require.NoError(t, dbx.DB.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":        gorm.Expr("quota + ?", 500),
		"aff_quota":    gorm.Expr("aff_quota - ?", 500),
		"display_name": "concurrent-update",
	}).Error)

	require.NoError(t, UpdateUserAccessToken(user.Id, "rotated-token"))

	var got model.User
	require.NoError(t, dbx.DB.First(&got, user.Id).Error)
	assert.Equal(t, "rotated-token", got.GetAccessToken())
	assert.Equal(t, "concurrent-update", got.DisplayName)
	assert.Equal(t, 1500, got.Quota)
	assert.Equal(t, 300, got.AffQuota)
	assert.Equal(t, 1200, got.AffHistoryQuota)
}

func TestUpdateUserAccessTokenRejectsSoftDeletedUser(t *testing.T) {
	setupUserStoreTestDB(t)

	user := model.User{
		Id:       3,
		Username: "deleted-token-rotation-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
	}
	user.SetAccessToken("old-token")
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, dbx.DB.Delete(&user).Error)

	err := UpdateUserAccessToken(user.Id, "orphaned-token")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var got model.User
	require.NoError(t, dbx.DB.Unscoped().First(&got, user.Id).Error)
	assert.Equal(t, "old-token", got.GetAccessToken())
}

func TestUpdateUserBindColumnOnlyTouchesTheBindingColumn(t *testing.T) {
	setupUserStoreTestDB(t)

	user := createUserBindTestUser(t)
	require.NoError(t, dbx.DB.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"role":   common.RoleAdminUser,
		"status": common.UserStatusEnabled,
		"group":  "vip",
	}).Error)

	require.NoError(t, UpdateUserBindColumn(user.Id, "github_id", "gh-12345"))

	reloaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "gh-12345", reloaded.GitHubId)
	assert.Equal(t, common.RoleAdminUser, reloaded.Role)
	assert.Equal(t, common.UserStatusEnabled, reloaded.Status)
	assert.Equal(t, "vip", reloaded.Group)
}

func TestUpdateUserBindColumnPreservesRestrictiveChange(t *testing.T) {
	setupUserStoreTestDB(t)

	user := createUserBindTestUser(t)
	require.NoError(t, dbx.DB.Model(&model.User{}).Where("id = ?", user.Id).
		Update("status", common.UserStatusDisabled).Error)
	require.NoError(t, UpdateUserBindColumn(user.Id, "wechat_id", "wx-open-id"))

	reloaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "wx-open-id", reloaded.WeChatId)
	assert.Equal(t, common.UserStatusDisabled, reloaded.Status)
}

func TestUpdateUserBindColumnRejectsNonWhitelistedColumns(t *testing.T) {
	setupUserStoreTestDB(t)

	user := createUserBindTestUser(t)
	for _, column := range []string{"role", "status", "group", "quota", "username", "password", "id"} {
		assert.Error(t, UpdateUserBindColumn(user.Id, column, "1"), "column %s must be rejected", column)
	}
	assert.Error(t, UpdateUserBindColumn(user.Id, "github_id; DROP TABLE users", "x"))
	assert.Error(t, UpdateUserBindColumn(0, "github_id", "x"))
}

func TestResetUserPasswordByEmailRequiresSingleActiveMatch(t *testing.T) {
	setupUserStoreTestDB(t)

	require.NoError(t, dbx.DB.Create(&model.User{
		Username: "duplicate-1",
		Password: "old-1",
		Email:    "legacy@example.com",
		AffCode:  "dupe1",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, dbx.DB.Create(&model.User{
		Username: "duplicate-2",
		Password: "old-2",
		Email:    "LEGACY@example.com",
		AffCode:  "dupe2",
		Status:   common.UserStatusEnabled,
	}).Error)

	err := ResetUserPasswordByEmail("legacy@example.com", "NewPassword123")
	require.ErrorIs(t, err, ErrEmailAmbiguous)

	var duplicates []model.User
	require.NoError(t, dbx.DB.Where("LOWER(email) = ?", "legacy@example.com").Order("username asc").Find(&duplicates).Error)
	require.Len(t, duplicates, 2)
	assert.Equal(t, "old-1", duplicates[0].Password)
	assert.Equal(t, "old-2", duplicates[1].Password)

	require.NoError(t, dbx.DB.Create(&model.User{
		Username: "unique",
		Password: "old",
		Email:    "unique@example.com",
		AffCode:  "unique",
		Status:   common.UserStatusEnabled,
	}).Error)

	require.NoError(t, ResetUserPasswordByEmail("UNIQUE@example.com", "NewPassword123"))

	var unique model.User
	require.NoError(t, dbx.DB.Where("username = ?", "unique").First(&unique).Error)
	assert.True(t, common.ValidatePasswordAndHash("NewPassword123", unique.Password))

	err = ResetUserPasswordByEmail("missing@example.com", "NewPassword123")
	require.True(t, errors.Is(err, ErrEmailNotFound))
}

func newStoreTestUserSession(sid string, userID int, now int64) *model.UserSession {
	return &model.UserSession{
		SID:             sid,
		UserID:          userID,
		Version:         1,
		UserAuthVersion: 1,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     fmt.Sprintf("current-%s", sid),
		LoginMethod:     "password",
		IP:              "127.0.0.1",
		UserAgent:       "store-test",
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}
}

func TestPasswordResetBumpsAuthVersionAndRevokesSessions(t *testing.T) {
	setupUserStoreTestDB(t)
	now := time.Now().Unix()
	user := &model.User{
		Username: "password-reset-user",
		Password: "old-hash",
		Email:    "password-reset@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, dbx.DB.Create(user).Error)
	t.Cleanup(func() { _ = dbx.DB.Unscoped().Delete(&model.User{}, user.Id).Error })
	session := newStoreTestUserSession("password-reset-session", user.Id, now)
	require.NoError(t, model.CreateUserSession(session))

	require.NoError(t, ResetUserPasswordByEmail(user.Email, "new-password"))
	var stored model.User
	require.NoError(t, dbx.DB.First(&stored, user.Id).Error)
	assert.Equal(t, int64(2), stored.AuthVersion)
	storedSession, err := model.GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusRevoked, storedSession.Status)
	assert.Equal(t, "password_reset", storedSession.RevokedReason)
}
