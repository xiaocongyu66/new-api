package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserUpdateTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)

	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}

func createUserBindTestUser(t *testing.T) User {
	t.Helper()
	user := User{
		Username:    "bind-test-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "bind-test-aff-code",
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func TestUserUpdateDoesNotOverwriteConcurrentAccountingOrTokenChanges(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:              1,
		Username:        "quota-race-user",
		Password:        "password",
		DisplayName:     "before",
		Status:          common.UserStatusEnabled,
		Quota:           1000,
		UsedQuota:       20,
		RequestCount:    3,
		AffCount:        2,
		AffQuota:        800,
		AffHistoryQuota: 1200,
	}
	user.SetAccessToken("old-token")
	require.NoError(t, DB.Create(&user).Error)

	staleUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 400),
		"used_quota":    gorm.Expr("used_quota + ?", 400),
		"request_count": gorm.Expr("request_count + ?", 1),
		"aff_count":     gorm.Expr("aff_count + ?", 1),
		"aff_quota":     gorm.Expr("aff_quota - ?", 500),
		"aff_history":   gorm.Expr("aff_history + ?", 500),
		"access_token":  "rotated-token",
	}).Error)

	staleUser.DisplayName = "after"
	require.NoError(t, staleUser.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "after", got.DisplayName)
	assert.Equal(t, 600, got.Quota)
	assert.Equal(t, 420, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
	assert.Equal(t, 3, got.AffCount)
	assert.Equal(t, 300, got.AffQuota)
	assert.Equal(t, 1700, got.AffHistoryQuota)
	assert.Equal(t, "rotated-token", got.GetAccessToken())
}

func TestUsageAccountingSupportsSignedDirectAndBatchDeltas(t *testing.T) {
	setupUserUpdateTestState(t)
	resetBatchUpdateTestState(t)

	user := User{
		Id:           10,
		Username:     "usage-adjustment-user",
		Password:     "password",
		Status:       common.UserStatusEnabled,
		UsedQuota:    1000,
		RequestCount: 3,
	}
	channel := Channel{
		Id:        10,
		Name:      "usage-adjustment-channel",
		Key:       "sk-test",
		Status:    common.ChannelStatusEnabled,
		UsedQuota: 1000,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&channel).Error)

	UpdateUserUsedQuota(user.Id, -200)
	UpdateUserUsedQuota(user.Id, 50)
	UpdateChannelUsedQuota(channel.Id, -200)
	UpdateChannelUsedQuota(channel.Id, 50)

	var got User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&got, user.Id).Error)
	assert.Equal(t, 850, got.UsedQuota)
	assert.Equal(t, 3, got.RequestCount)
	var gotChannel Channel
	require.NoError(t, DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(850), gotChannel.UsedQuota)

	common.BatchUpdateEnabled = true
	UpdateUserUsedQuota(user.Id, 400)
	UpdateUserUsedQuota(user.Id, -100)
	UpdateChannelUsedQuota(channel.Id, 400)
	UpdateChannelUsedQuota(channel.Id, -100)

	require.NoError(t, DB.Select("used_quota", "request_count").First(&got, user.Id).Error)
	assert.Equal(t, 850, got.UsedQuota, "batch deltas must remain queued until flush")
	assert.Equal(t, 3, got.RequestCount)
	require.NoError(t, DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(850), gotChannel.UsedQuota, "batch deltas must remain queued until flush")

	batchUpdate()
	require.NoError(t, DB.Select("used_quota", "request_count").First(&got, user.Id).Error)
	assert.Equal(t, 1150, got.UsedQuota)
	assert.Equal(t, 3, got.RequestCount)
	require.NoError(t, DB.Select("used_quota").First(&gotChannel, channel.Id).Error)
	assert.Equal(t, int64(1150), gotChannel.UsedQuota)
}

func TestUpdateUserSettingOnlyUpdatesSetting(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:           2,
		Username:     "setting-user",
		Password:     "password",
		Status:       common.UserStatusEnabled,
		Quota:        1000,
		UsedQuota:    20,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 250),
		"used_quota":    gorm.Expr("used_quota + ?", 250),
		"request_count": gorm.Expr("request_count + ?", 1),
	}).Error)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 750, got.Quota)
	assert.Equal(t, 270, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
	assert.Equal(t, "zh", got.GetSetting().Language)
}

func TestEnsureEmailAvailableRejectsExistingEmailCaseInsensitive(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "existing",
		Password: "old-password",
		Email:    "Taken@Example.com",
		Status:   common.UserStatusEnabled,
	}).Error)

	err := EnsureEmailAvailable(" taken@example.COM ", 0)
	require.ErrorIs(t, err, ErrEmailAlreadyTaken)

	var users []User
	require.NoError(t, DB.Where("LOWER(email) = ?", "taken@example.com").Limit(1).Find(&users).Error)
	require.Len(t, users, 1)

	require.NoError(t, EnsureEmailAvailable("taken@example.com", users[0].Id))
}

func TestInsertRejectsDuplicateEmailWithoutUniqueIndex(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "existing",
		Password: "old-password",
		Email:    "taken@example.com",
		Status:   common.UserStatusEnabled,
	}).Error)

	user := &User{
		Username: "oauth-user",
		Email:    "TAKEN@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}

	err := user.Insert(0)
	require.ErrorIs(t, err, ErrEmailAlreadyTaken)

	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "oauth-user").Count(&count).Error)
	assert.Zero(t, count)
}

func TestInsertKeepsBlankPasswordForPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)

	user := &User{
		Username: "passwordless-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}

	require.NoError(t, user.Insert(0))

	var stored User
	require.NoError(t, DB.Where("username = ?", user.Username).First(&stored).Error)
	assert.Empty(t, stored.Password)
}

func TestValidateAndFillRejectsPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "passwordless-user",
		Password: "",
		Status:   common.UserStatusEnabled,
	}).Error)

	loginUser := User{
		Username: "passwordless-user",
		Password: "NewPassword123",
	}
	err := loginUser.ValidateAndFill()
	require.ErrorIs(t, err, ErrInvalidCredentials)

	var stored User
	require.NoError(t, DB.Where("username = ?", "passwordless-user").First(&stored).Error)
	assert.Empty(t, stored.Password)
}
