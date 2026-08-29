package identity

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthLogoutRejectsRefreshCookieSessionMismatch(t *testing.T) {
	previousDB := dbx.DB
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &UserSession{}))
	dbx.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "auth-logout-mismatch-test-secret"
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
	})

	user := &User{
		Username: "logout-mismatch-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	sessionA, err := CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-a")
	require.NoError(t, err)
	sessionB, err := CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-b")
	require.NoError(t, err)

	c, recorder := ginadapter.NewSyntheticContext(nil)
	ginadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodPost, "/api/user/auth/logout", nil))
	c.Headers().Set("Authorization", "Bearer "+sessionA.AccessToken)
	c.Headers().Set("X-Auth-Session", sessionA.Session.SID)
	c.HTTPRequest().AddCookie(&http.Cookie{Name: RefreshCookieName, Value: sessionB.RefreshToken})

	AuthLogout(c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "AUTH_SESSION_MISMATCH", response.Code)
	for _, sid := range []string{sessionA.Session.SID, sessionB.Session.SID} {
		stored, err := GetUserSessionBySID(sid)
		require.NoError(t, err)
		assert.Equal(t, UserSessionStatusActive, stored.Status)
	}
}

func TestWriteAuthSessionErrorMapsSessionGrowthLimits(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "active session limit",
			err:            ErrUserSessionLimit,
			expectedStatus: http.StatusConflict,
			expectedCode:   "AUTH_SESSION_LIMIT",
		},
		{
			name:           "issuance limit",
			err:            ErrUserSessionIssuanceLimit,
			expectedStatus: http.StatusTooManyRequests,
			expectedCode:   "AUTH_SESSION_ISSUANCE_LIMIT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder := ginadapter.NewSyntheticContext(nil)
			writeAuthSessionError(c, test.err)

			assert.Equal(t, test.expectedStatus, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, test.expectedCode, response.Code)
		})
	}
}

func TestSessionLimitDoesNotRecordRejectedLoginAsSuccessful(t *testing.T) {
	previousDB := dbx.DB
	previousRedis := common.RedisEnabled
	previousActiveLimit := common.UserSessionActiveLimit
	previousIssuanceLimit := common.UserSessionIssuanceLimit
	previousIssuanceWindow := common.UserSessionIssuanceWindowSeconds
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &UserSession{}))
	dbx.DB = db
	common.RedisEnabled = false
	common.UserSessionActiveLimit = 1
	common.UserSessionIssuanceLimit = 100
	common.UserSessionIssuanceWindowSeconds = int64(common.DefaultUserSessionIssuanceWindowSeconds)
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.RedisEnabled = previousRedis
		common.UserSessionActiveLimit = previousActiveLimit
		common.UserSessionIssuanceLimit = previousIssuanceLimit
		common.UserSessionIssuanceWindowSeconds = previousIssuanceWindow
	})

	const previousLastLoginAt = int64(123)
	user := &User{
		Username: "rejected-login-audit-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, LastLoginAt: previousLastLoginAt,
	}
	require.NoError(t, db.Create(user).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&UserSession{
		SID: "existing-active-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: UserSessionStatusActive, RefreshHash: "hash", LoginMethod: "password",
		CreatedAt: now, LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	c, recorder := ginadapter.NewSyntheticContext(nil)
	ginadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodPost, "/api/user/login", nil))
	SetupLogin(user, c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, previousLastLoginAt, stored.LastLoginAt)
}
