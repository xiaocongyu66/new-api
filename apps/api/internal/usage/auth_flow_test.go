package usage

import (
	"errors"
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/glebarez/sqlite"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthFlowIsBoundAndConsumedOnce(t *testing.T) {
	truncateTables(t)

	token, created, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose:   identity.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identity.AuthFlowIntentBind,
		UserId:    42,
		SessionId: "identity.session-a",
		Payload:   `{"affiliate_code":"invite"}`,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.NotEqual(t, token, created.TokenHash)

	_, err = identity.ConsumeAuthFlow(token, identity.AuthFlowMatch{
		Purpose:   identity.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identity.AuthFlowIntentBind,
		UserId:    99,
		SessionId: "identity.session-a",
	})
	assert.ErrorIs(t, err, identity.ErrAuthFlowInvalid)

	peeked, err := identity.GetAuthFlow(token, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeOAuth, Provider: "github"})
	require.NoError(t, err)
	assert.Nil(t, peeked.ConsumedAt)

	consumed, err := identity.ConsumeAuthFlow(token, identity.AuthFlowMatch{
		Purpose:   identity.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identity.AuthFlowIntentBind,
		UserId:    42,
		SessionId: "identity.session-a",
	})
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)

	_, err = identity.ConsumeAuthFlow(token, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, identity.ErrAuthFlowConsumed)
}

func TestAuthFlowExpiryIsEnforced(t *testing.T) {
	truncateTables(t)

	token, flow, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose:   identity.AuthFlowPurposeTwoFALogin,
		UserId:    7,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, dbx.DB.Model(&identity.AuthFlow{}).Where("id = ?", flow.Id).Update("expires_at", time.Now().Add(-time.Second)).Error)

	_, err = identity.GetAuthFlow(token, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeTwoFALogin})
	assert.True(t, errors.Is(err, identity.ErrAuthFlowExpired))
	_, err = identity.ConsumeAuthFlow(token, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeTwoFALogin})
	assert.True(t, errors.Is(err, identity.ErrAuthFlowExpired))
}

func TestExternalAuthAssertionCanOnlyBeClaimedOnce(t *testing.T) {
	truncateTables(t)
	expiresAt := time.Now().Add(time.Minute)

	require.NoError(t, identity.ClaimExternalAuthAssertion(identity.AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt))
	err := identity.ClaimExternalAuthAssertion(identity.AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt)
	assert.ErrorIs(t, err, identity.ErrAuthFlowConsumed)

	require.NoError(t, identity.ClaimExternalAuthAssertion(identity.AuthFlowPurposeTelegramAssertion, "different-assertion", expiresAt))
}

func TestConsumeAuthFlowWithActionRollsBackTogether(t *testing.T) {
	truncateTables(t)
	token, _, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose:   identity.AuthFlowPurposeTelegramBind,
		UserId:    42,
		SessionId: "identity.session-a",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	actionErr := errors.New("binding failed")

	_, err = identity.ConsumeAuthFlowWithAction(token, identity.AuthFlowMatch{
		Purpose: identity.AuthFlowPurposeTelegramBind, UserId: 42, SessionId: "identity.session-a",
	}, func(tx *gorm.DB, _ *identity.AuthFlow) error {
		if err := identity.ClaimExternalAuthAssertionWithTx(tx, identity.AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)); err != nil {
			return err
		}
		return actionErr
	})
	assert.ErrorIs(t, err, actionErr)

	flow, err := identity.GetAuthFlow(token, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, flow.ConsumedAt)
	require.NoError(t, identity.ClaimExternalAuthAssertion(identity.AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)))
}

func truncateTables(t *testing.T) {
	t.Helper()
	setupIdentityTestDB(t)
}

func setupIdentityTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	previousRedis := common.RedisEnabled
	previousBatch := common.BatchUpdateEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	if err := db.AutoMigrate(
		&identity.User{},
		&identity.UserSession{},
		&identity.AuthFlow{},
		&identity.ExternalIdentityClaim{},
		&identity.Token{},
		&identity.PasskeyCredential{},
		&identity.TwoFA{},
		&identity.TwoFABackupCode{},
		&identity.UserOAuthBinding{},
		&identity.CustomOAuthProvider{},
		// usage's own records: the tests that read them used to ride on the
		// model package's shared TestMain before these files moved here.
		&Log{},
		&QuotaData{},
		&catalog.Channel{},
	); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	t.Cleanup(func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		common.BatchUpdateEnabled = previousBatch
		_ = sqlDB.Close()
	})
}
