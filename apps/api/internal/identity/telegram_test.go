package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVerifyTelegramAuthorization(t *testing.T) {
	const token = "telegram-test-token"
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name     string
		authDate time.Time
		mutate   func(url.Values)
		wantID   string
		wantErr  string
	}{
		{name: "valid", authDate: now, wantID: "123456"},
		{name: "small future clock skew", authDate: now.Add(90 * time.Second), wantID: "123456"},
		{name: "expired", authDate: now.Add(-telegramAuthorizationMaxAge - time.Second), wantErr: "expired"},
		{name: "too far in future", authDate: now.Add(telegramAuthorizationFutureSkew + time.Second), wantErr: "expired"},
		{name: "invalid signature", authDate: now, mutate: func(values url.Values) { values.Set("hash", "00") }, wantErr: "signature"},
		{name: "unsigned flow token query is rejected", authDate: now, mutate: func(values url.Values) { values.Set("flow_token", "must-be-in-path") }, wantErr: "signature"},
		{name: "duplicate parameter", authDate: now, mutate: func(values url.Values) { values["id"] = append(values["id"], "654321") }, wantErr: "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := signedTelegramAuthorization(token, tt.authDate)
			if tt.mutate != nil {
				tt.mutate(params)
			}

			telegramID, err := verifyTelegramAuthorization(params, token, now)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, telegramID)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, telegramID)
		})
	}
}

func signedTelegramAuthorization(token string, authDate time.Time) url.Values {
	params := url.Values{
		"auth_date":  {strconv.FormatInt(authDate.Unix(), 10)},
		"first_name": {"Test"},
		"id":         {"123456"},
	}
	signTelegramAuthorization(token, params)
	return params
}

func signTelegramAuthorization(token string, params url.Values) {
	keys := make([]string, 0, len(params))
	for key := range params {
		if key == "hash" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	dataCheck := make([]string, 0, len(keys))
	for _, key := range keys {
		dataCheck = append(dataCheck, key+"="+params.Get(key))
	}
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write([]byte(strings.Join(dataCheck, "\n")))
	params.Set("hash", hex.EncodeToString(mac.Sum(nil)))
}

func createTelegramBindTestFlow(t *testing.T, db *gorm.DB, name string, status int, now time.Time) (*User, string) {
	t.Helper()
	user := &User{
		Username: name, Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: status, Group: "default", AuthVersion: 1, AffCode: name,
	}
	require.NoError(t, db.Create(user).Error)
	session := &UserSession{
		SID: name + "-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: UserSessionStatusActive, RefreshHash: name + "-refresh-hash", LoginMethod: "password",
		CreatedAt: now.Unix(), LastActiveAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, CreateUserSession(session))
	flowToken, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: user.Id, SessionId: session.SID,
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	return user, flowToken
}

func assertTelegramBindRedirect(t *testing.T, response *httptest.ResponseRecorder, flowToken, errorCode string) {
	t.Helper()
	require.Equal(t, http.StatusFound, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/oauth/telegram", location.Path)
	assert.Equal(t, "error", location.Query().Get("telegram_bind"))
	assert.Equal(t, flowToken, location.Query().Get("flow_token"))
	assert.Equal(t, errorCode, location.Query().Get("error_code"))
	assert.Empty(t, location.Query().Get("error_description"))
	assert.Empty(t, location.Query().Get("message"))
}

func TestTelegramBindFailureResponseContract(t *testing.T) {
	failures := []struct {
		name      string
		errorCode string
	}{
		{name: "disabled", errorCode: telegramBindErrorDisabled},
		{name: "invalid request", errorCode: telegramBindErrorInvalidRequest},
		{name: "invalid flow", errorCode: telegramBindErrorFlowInvalid},
		{name: "invalid session", errorCode: telegramBindErrorSessionInvalid},
		{name: "already bound", errorCode: telegramBindErrorAlreadyBound},
		{name: "deleted user", errorCode: telegramBindErrorUserDeleted},
		{name: "disabled user", errorCode: telegramBindErrorUserDisabled},
		{name: "internal error", errorCode: telegramBindErrorInternal},
	}

	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			contextRaw, _ := gin.CreateTestContext(response)
			contextRaw.Params = gin.Params{{Key: "flow_token", Value: "flow token"}}
			contextRaw.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/flow-token", nil)
			context := ginadapter.Wrap(contextRaw)

			telegramBindFailure(context, failure.errorCode)

			assertTelegramBindRedirect(t, response, "flow token", failure.errorCode)
		})
	}
}

func TestTelegramBindCommitsFlowAssertionAndBindingAtomically(t *testing.T) {
	previousDB := dbx.DB
	previousType := common.MainDatabaseType()
	previousRedis := common.RedisEnabled
	previousEnabled := common.TelegramOAuthEnabled
	previousToken := common.TelegramBotToken
	previousSecret := common.SessionSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
	))
	dbx.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.TelegramOAuthEnabled = true
	common.TelegramBotToken = "telegram-bind-test-token"
	common.SessionSecret = "telegram-bind-session-secret"
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.RedisEnabled = previousRedis
		common.TelegramOAuthEnabled = previousEnabled
		common.TelegramBotToken = previousToken
		common.SessionSecret = previousSecret
	})

	user := &User{
		Username: "telegram-bind-user", Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "telegram-bind-user",
	}
	require.NoError(t, db.Create(user).Error)
	now := time.Now()
	session := &UserSession{
		SID: "telegram-bind-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		CreatedAt: now.Unix(), LastActiveAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, CreateUserSession(session))
	flowToken, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: user.Id, SessionId: session.SID,
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	params := signedTelegramAuthorization(common.TelegramBotToken, now)
	router := gin.New()
	router.GET("/api/oauth/telegram/bind/:flow_token", ginadapter.Handler(TelegramBind))

	common.TelegramOAuthEnabled = false
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/disabled-flow", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, "disabled-flow", telegramBindErrorDisabled)
	common.TelegramOAuthEnabled = true

	request = httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/invalid-request", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, "invalid-request", telegramBindErrorInvalidRequest)

	request = httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/missing-flow?"+params.Encode(), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, "missing-flow", telegramBindErrorFlowInvalid)

	invalidSessionFlowToken, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: user.Id, SessionId: "missing-session",
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/telegram/bind/"+invalidSessionFlowToken+"?"+params.Encode(),
		nil,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, invalidSessionFlowToken, telegramBindErrorSessionInvalid)
	invalidSessionFlow, err := GetAuthFlow(invalidSessionFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, invalidSessionFlow.ConsumedAt)

	request = httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/"+flowToken+"?"+params.Encode(), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "/oauth/telegram?telegram_bind=success&flow_token="+url.QueryEscape(flowToken), response.Header().Get("Location"))
	var storedUser User
	require.NoError(t, db.First(&storedUser, user.Id).Error)
	assert.Equal(t, "123456", storedUser.TelegramId)
	var identityClaim ExternalIdentityClaim
	require.NoError(t, db.Where("provider = ? AND subject = ?", ExternalIdentityProviderTelegram, "123456").
		First(&identityClaim).Error)
	assert.Equal(t, user.Id, identityClaim.UserId)
	_, err = GetAuthFlow(flowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	assert.ErrorIs(t, err, ErrAuthFlowConsumed)

	replayFlowToken, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: user.Id, SessionId: session.SID,
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind/"+replayFlowToken+"?"+params.Encode(), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, replayFlowToken, telegramBindErrorInvalidRequest)
	replayFlow, err := GetAuthFlow(replayFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, replayFlow.ConsumedAt)

	competingUser := &User{
		Username: "telegram-bind-competing-user", Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "telegram-bind-competing-user",
	}
	require.NoError(t, db.Create(competingUser).Error)
	competingSession := &UserSession{
		SID: "telegram-bind-competing-session", UserID: competingUser.Id, Version: 1,
		UserAuthVersion: competingUser.AuthVersion, Status: UserSessionStatusActive,
		RefreshHash: "competing-refresh-hash", LoginMethod: "password",
		CreatedAt: now.Unix(), LastActiveAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, CreateUserSession(competingSession))
	competingFlowToken, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTelegramBind, UserId: competingUser.Id, SessionId: competingSession.SID,
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)
	competingParams := signedTelegramAuthorization(common.TelegramBotToken, now)
	competingParams.Set("first_name", "Competing")
	signTelegramAuthorization(common.TelegramBotToken, competingParams)
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/telegram/bind/"+competingFlowToken+"?"+competingParams.Encode(),
		nil,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, competingFlowToken, telegramBindErrorAlreadyBound)

	require.NoError(t, db.First(competingUser, competingUser.Id).Error)
	assert.Empty(t, competingUser.TelegramId)
	competingFlow, err := GetAuthFlow(competingFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, competingFlow.ConsumedAt)
	competingAssertion, competingAssertionExpiry, err := telegramAuthorizationClaim(competingParams, time.Now())
	require.NoError(t, err)
	require.NoError(t, ClaimExternalAuthAssertion(
		AuthFlowPurposeTelegramAssertion,
		competingAssertion,
		competingAssertionExpiry,
	))

	disabledUser, disabledFlowToken := createTelegramBindTestFlow(
		t, db, "telegram-bind-disabled-user", common.UserStatusDisabled, now,
	)
	disabledParams := signedTelegramAuthorization(common.TelegramBotToken, now)
	disabledParams.Set("id", "disabled-telegram-id")
	disabledParams.Set("first_name", "Disabled")
	signTelegramAuthorization(common.TelegramBotToken, disabledParams)
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/telegram/bind/"+disabledFlowToken+"?"+disabledParams.Encode(),
		nil,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, disabledFlowToken, telegramBindErrorUserDisabled)
	var storedDisabledUser User
	require.NoError(t, db.First(&storedDisabledUser, disabledUser.Id).Error)
	assert.Empty(t, storedDisabledUser.TelegramId)
	disabledFlow, err := GetAuthFlow(disabledFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, disabledFlow.ConsumedAt)
	disabledAssertion, disabledAssertionExpiry, err := telegramAuthorizationClaim(disabledParams, time.Now())
	require.NoError(t, err)
	require.NoError(t, ClaimExternalAuthAssertion(
		AuthFlowPurposeTelegramAssertion,
		disabledAssertion,
		disabledAssertionExpiry,
	))

	deletedUser, deletedFlowToken := createTelegramBindTestFlow(
		t, db, "telegram-bind-deleted-user", common.UserStatusEnabled, now,
	)
	require.NoError(t, db.Delete(deletedUser).Error)
	deletedParams := signedTelegramAuthorization(common.TelegramBotToken, now)
	deletedParams.Set("id", "deleted-telegram-id")
	deletedParams.Set("first_name", "Deleted")
	signTelegramAuthorization(common.TelegramBotToken, deletedParams)
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/telegram/bind/"+deletedFlowToken+"?"+deletedParams.Encode(),
		nil,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertTelegramBindRedirect(t, response, deletedFlowToken, telegramBindErrorUserDeleted)
	deletedFlow, err := GetAuthFlow(deletedFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, deletedFlow.ConsumedAt)
	deletedAssertion, deletedAssertionExpiry, err := telegramAuthorizationClaim(deletedParams, time.Now())
	require.NoError(t, err)
	require.NoError(t, ClaimExternalAuthAssertion(
		AuthFlowPurposeTelegramAssertion,
		deletedAssertion,
		deletedAssertionExpiry,
	))

	_, internalFlowToken := createTelegramBindTestFlow(
		t, db, "telegram-bind-internal-error", common.UserStatusEnabled, now,
	)
	internalParams := signedTelegramAuthorization(common.TelegramBotToken, now)
	internalParams.Set("id", "internal-error-telegram-id")
	internalParams.Set("first_name", "Internal")
	signTelegramAuthorization(common.TelegramBotToken, internalParams)
	forcedError := errors.New("forced telegram session query failure")
	const callbackName = "test:telegram-bind-session-query-failure"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "user_sessions" {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); inTransaction {
			tx.AddError(forcedError)
		}
	}))
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/telegram/bind/"+internalFlowToken+"?"+internalParams.Encode(),
		nil,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	db.Callback().Query().Remove(callbackName)
	assertTelegramBindRedirect(t, response, internalFlowToken, telegramBindErrorInternal)
	assert.NotContains(t, response.Header().Get("Location"), forcedError.Error())
	internalFlow, err := GetAuthFlow(internalFlowToken, AuthFlowMatch{Purpose: AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, internalFlow.ConsumedAt)
	internalAssertion, internalAssertionExpiry, err := telegramAuthorizationClaim(internalParams, time.Now())
	require.NoError(t, err)
	require.NoError(t, ClaimExternalAuthAssertion(
		AuthFlowPurposeTelegramAssertion,
		internalAssertion,
		internalAssertionExpiry,
	))

}
