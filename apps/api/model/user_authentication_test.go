package model

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"context"
	"encoding/base64"
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/go-redis/redis/v8"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHardDeleteUserFailsClosedWhenAuthFenceCannotPublish(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "hard-delete-user", Password: "password", TelegramId: "hard-delete-telegram"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))
	require.NoError(t, dbx.DB.Create(&identity.Token{UserId: user.Id, Key: "hard-delete-token"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}).Error)
	require.NoError(t, dbx.DB.Create(&identity.TwoFABackupCode{UserId: user.Id, CodeHash: "hash"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.PasskeyCredential{UserID: user.Id, CredentialID: "credential", PublicKey: "public-key"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.UserOAuthBinding{UserId: user.Id, ProviderId: 1, ProviderUserId: "provider-user"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.UserSession{
		SID: "hard-delete-identity.session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: identity.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: 1, ExpiresAt: 2,
	}).Error)
	require.NoError(t, dbx.DB.Create(&identity.AuthFlow{
		TokenHash: "hard-delete-auth-flow", Purpose: identity.AuthFlowPurposeTwoFALogin,
		UserId: user.Id, ExpiresAt: time.Now().Add(time.Minute),
	}).Error)

	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("forced redis failure")
		},
		MaxRetries: -1,
	})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	require.Error(t, (&identity.User{Id: user.Id}).identity.HardDelete())

	var count int64
	require.NoError(t, dbx.DB.Unscoped().Model(&identity.User{}).Where("id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	for _, record := range []any{
		&identity.Token{},
		&identity.TwoFA{},
		&identity.TwoFABackupCode{},
		&identity.PasskeyCredential{},
		&identity.UserOAuthBinding{},
		&identity.UserSession{},
		&identity.AuthFlow{},
		&identity.ExternalIdentityClaim{},
	} {
		require.NoError(t, dbx.DB.Unscoped().Model(record).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
}

func TestHardDeleteUserPublishesTombstoneAndPurgesAuthenticationData(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)

	user := identity.User{
		Username: "hard-delete-success", Password: "password", AuthVersion: 1,
		TelegramId: "hard-delete-success-telegram",
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))
	require.NoError(t, dbx.DB.Create(&identity.Token{UserId: user.Id, Key: "hard-delete-success-token"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}).Error)
	require.NoError(t, dbx.DB.Create(&identity.TwoFABackupCode{UserId: user.Id, CodeHash: "hash"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.PasskeyCredential{UserID: user.Id, CredentialID: "credential-success", PublicKey: "public-key"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.UserOAuthBinding{UserId: user.Id, ProviderId: 1, ProviderUserId: "provider-user-success"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.UserSession{
		SID: "hard-delete-success-identity.session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: identity.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: 1, ExpiresAt: 2,
	}).Error)
	require.NoError(t, dbx.DB.Create(&identity.AuthFlow{
		TokenHash: "hard-delete-success-flow", Purpose: identity.AuthFlowPurposeTwoFALogin,
		UserId: user.Id, ExpiresAt: time.Now().Add(time.Minute),
	}).Error)
	require.NoError(t, identity.populateUserCache(user))
	// Administrative hard deletion commonly targets an already soft-deleted
	// user; the shared version increment must therefore query unscoped.
	require.NoError(t, dbx.DB.identity.Delete(&user).Error)

	require.NoError(t, (&identity.User{Id: user.Id}).identity.HardDelete())

	var count int64
	require.NoError(t, dbx.DB.Unscoped().Model(&identity.User{}).Where("id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	for _, record := range []any{
		&identity.Token{},
		&identity.TwoFA{},
		&identity.TwoFABackupCode{},
		&identity.PasskeyCredential{},
		&identity.UserOAuthBinding{},
		&identity.UserSession{},
		&identity.AuthFlow{},
		&identity.ExternalIdentityClaim{},
	} {
		require.NoError(t, dbx.DB.Unscoped().Model(record).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Zero(t, count)
	}
	assert.False(t, server.Exists(identity.getUserAuthFenceKey(user.Id)))
	committed, err := common.RDB.Get(t.Context(), identity.getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)
	assert.False(t, server.Exists(quotacache.UserKey(user.Id)))
}

func TestIncrementFailedAttemptsCountsConcurrentFailures(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "twofa-cas-user", Password: "password"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	twoFA := identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}
	require.NoError(t, dbx.DB.Create(&twoFA).Error)

	const attempts = 4
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- (&identity.TwoFA{Id: twoFA.Id}).identity.IncrementFailedAttempts()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var reloaded identity.TwoFA
	require.NoError(t, dbx.DB.First(&reloaded, twoFA.Id).Error)
	assert.Equal(t, attempts, reloaded.FailedAttempts)
}

func TestValidateBackupCodeCanOnlySucceedOnce(t *testing.T) {
	truncateTables(t)

	const code = "ABCD-1234"
	user := identity.User{Id: 123, Username: "backup-code-user", Password: "password", AuthVersion: 1}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, dbx.DB.Create(&identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: false}).Error)
	require.NoError(t, identity.CreatePendingTwoFASetupBackupCodes(user.Id, []string{code}))

	const attempts = 2
	results := make(chan bool, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			valid, err := identity.ValidateBackupCode(123, code)
			results <- valid
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	wins := 0
	for valid := range results {
		if valid {
			wins++
		}
	}
	assert.Equal(t, 1, wins)

	remaining, err := identity.GetUnusedBackupCodeCount(123)
	require.NoError(t, err)
	assert.Zero(t, remaining)
}

func TestPendingTwoFASetupAPIsRejectEnabledFactor(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "enabled-twofa-guard", Password: "password", AuthVersion: 1}
	require.NoError(t, dbx.DB.Create(&user).Error)
	twoFA := identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}
	require.NoError(t, dbx.DB.Create(&twoFA).Error)

	require.Error(t, identity.CreatePendingTwoFASetupBackupCodes(user.Id, []string{"ABCD-1234"}))
	require.Error(t, twoFA.identity.DeletePendingTwoFASetup())

	var stored identity.TwoFA
	require.NoError(t, dbx.DB.First(&stored, twoFA.Id).Error)
	assert.True(t, stored.IsEnabled)
	var backupCodeCount int64
	require.NoError(t, dbx.DB.Model(&identity.TwoFABackupCode{}).Where("user_id = ?", user.Id).Count(&backupCodeCount).Error)
	assert.Zero(t, backupCodeCount)
}

func TestSecurityFactorMutationsAdvanceUserAuthVersion(t *testing.T) {
	truncateTables(t)

	user := identity.User{
		Username:    "security-factor-version-user",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	twoFA := identity.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: false}
	require.NoError(t, dbx.DB.Create(&twoFA).Error)

	require.NoError(t, twoFA.identity.EnableWithAuthVersion())
	assertUserAuthVersion(t, user.Id, 2)
	assert.ErrorIs(t, twoFA.identity.EnableWithAuthVersion(), identity.ErrTwoFAAlreadyEnabled)
	assertUserAuthVersion(t, user.Id, 2)
	require.NoError(t, identity.ReplaceBackupCodesWithAuthVersion(user.Id, []string{"ABCD-1234"}))
	assertUserAuthVersion(t, user.Id, 3)
	require.NoError(t, identity.DisableTwoFAWithAuthVersion(user.Id))
	assertUserAuthVersion(t, user.Id, 4)

	credential := &identity.PasskeyCredential{UserID: user.Id, CredentialID: "credential-id", PublicKey: "public-key"}
	require.NoError(t, identity.UpsertPasskeyCredentialWithAuthVersion(credential))
	assertUserAuthVersion(t, user.Id, 5)
	require.NoError(t, identity.DeletePasskeyByUserIDWithAuthVersion(user.Id))
	assertUserAuthVersion(t, user.Id, 6)
}

func TestUpdatePasskeyAssertionStateCannotRewriteRegistrationIdentity(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "passkey-assertion-state", Password: "password", AuthVersion: 1}
	require.NoError(t, dbx.DB.Create(&user).Error)
	credentialID := []byte("stable-credential-id")
	stored := identity.PasskeyCredential{
		UserID:          user.Id,
		CredentialID:    base64.StdEncoding.EncodeToString(credentialID),
		PublicKey:       "original-public-key",
		AttestationType: "packed",
		AAGUID:          "original-aaguid",
		SignCount:       1,
		Transports:      `["usb"]`,
		Attachment:      "platform",
	}
	require.NoError(t, dbx.DB.Create(&stored).Error)
	usedAt := time.Now().UTC().Truncate(time.Second)
	validated := &webauthn.Credential{
		ID:              credentialID,
		PublicKey:       []byte("replacement-public-key"),
		AttestationType: "none",
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:       []byte("replacement-aaguid"),
			SignCount:    8,
			CloneWarning: true,
		},
	}
	require.NoError(t, identity.UpdatePasskeyAssertionState(user.Id, validated, usedAt))

	var updated identity.PasskeyCredential
	require.NoError(t, dbx.DB.First(&updated, stored.ID).Error)
	assert.Equal(t, stored.CredentialID, updated.CredentialID)
	assert.Equal(t, stored.PublicKey, updated.PublicKey)
	assert.Equal(t, stored.AttestationType, updated.AttestationType)
	assert.Equal(t, stored.AAGUID, updated.AAGUID)
	assert.Equal(t, stored.Transports, updated.Transports)
	assert.Equal(t, stored.Attachment, updated.Attachment)
	assert.EqualValues(t, 8, updated.SignCount)
	assert.True(t, updated.CloneWarning)
	assert.True(t, updated.UserPresent)
	assert.True(t, updated.UserVerified)
	assert.True(t, updated.BackupEligible)
	assert.True(t, updated.BackupState)
	require.NotNil(t, updated.LastUsedAt)
	assert.Equal(t, usedAt.Unix(), updated.LastUsedAt.Unix())

	validated.ID = []byte("another-credential")
	assert.ErrorIs(t, identity.UpdatePasskeyAssertionState(user.Id, validated, usedAt), identity.ErrPasskeyNotFound)
}

func assertUserAuthVersion(t *testing.T, userID int, expected int64) {
	t.Helper()
	var version int64
	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", userID).Select("auth_version").Scan(&version).Error)
	assert.Equal(t, expected, version)
}
