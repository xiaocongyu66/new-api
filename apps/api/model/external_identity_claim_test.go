package model

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExternalIdentityClaimEnforcesSingleOwnerAtomically(t *testing.T) {
	truncateTables(t)

	first := identity.User{Username: "telegram-owner-one", Password: "password", AffCode: "telegram-owner-one"}
	second := identity.User{Username: "telegram-owner-two", Password: "password", AffCode: "telegram-owner-two"}
	require.NoError(t, dbx.DB.Create(&first).Error)
	require.NoError(t, dbx.DB.Create(&second).Error)

	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, "telegram-123", first.Id)
	}))
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, "telegram-123", second.Id)
	})
	assert.ErrorIs(t, err, identity.ErrExternalIdentityAlreadyClaimed)

	err = dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, "telegram-456", first.Id)
	})
	assert.ErrorIs(t, err, identity.ErrExternalIdentityAlreadyClaimed)

	var claims []identity.ExternalIdentityClaim
	require.NoError(t, dbx.DB.Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, first.Id, claims[0].UserId)
	assert.Equal(t, "telegram-123", claims[0].Subject)

	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ReleaseExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, first.Id)
	}))
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, "telegram-123", second.Id)
	}))
}

func TestClearTelegramBindingReleasesIdentityClaim(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "telegram-unbind", Password: "password", TelegramId: "telegram-unbind-id"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, dbx.DB.Transaction(func(tx *gorm.DB) error {
		return identity.ClaimExternalIdentityWithTx(tx, identity.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))

	require.NoError(t, user.identity.ClearBinding(identity.ExternalIdentityProviderTelegram))
	assert.Empty(t, user.TelegramId)

	var count int64
	require.NoError(t, dbx.DB.Model(&identity.ExternalIdentityClaim{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestInitializeExternalIdentityClaimsIsIdempotent(t *testing.T) {
	truncateTables(t)

	user := identity.User{Username: "telegram-legacy", Password: "password", TelegramId: "telegram-legacy-id"}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, identity.InitializeExternalIdentityClaims())
	require.NoError(t, identity.InitializeExternalIdentityClaims())

	var claim identity.ExternalIdentityClaim
	require.NoError(t, dbx.DB.Where("provider = ? AND subject = ?", identity.ExternalIdentityProviderTelegram, user.TelegramId).
		First(&claim).Error)
	assert.Equal(t, user.Id, claim.UserId)
}

func TestInitializeExternalIdentityClaimsRejectsAmbiguousLegacyBindings(t *testing.T) {
	truncateTables(t)

	first := identity.User{Username: "telegram-legacy-one", Password: "password", TelegramId: "duplicate-telegram-id", AffCode: "telegram-legacy-one"}
	second := identity.User{Username: "telegram-legacy-two", Password: "password", TelegramId: "duplicate-telegram-id", AffCode: "telegram-legacy-two"}
	require.NoError(t, dbx.DB.Create(&first).Error)
	require.NoError(t, dbx.DB.Create(&second).Error)

	err := identity.InitializeExternalIdentityClaims()
	assert.ErrorIs(t, err, identity.ErrExternalIdentityAlreadyClaimed)

	var count int64
	require.NoError(t, dbx.DB.Model(&identity.ExternalIdentityClaim{}).Count(&count).Error)
	assert.Zero(t, count)
}
