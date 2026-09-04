package handler

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/security/oauth"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type authFlowTestOAuthProvider struct {
	exchangeErr   error
	userInfoErr   error
	exchangeCalls int
	userInfoCalls int
}

func (*authFlowTestOAuthProvider) GetName() string { return "Auth Flow Test" }
func (*authFlowTestOAuthProvider) IsEnabled() bool { return true }
func (provider *authFlowTestOAuthProvider) ExchangeToken(context.Context, string, contract.Context) (*oauth.OAuthToken, error) {
	provider.exchangeCalls++
	if provider.exchangeErr != nil {
		return nil, provider.exchangeErr
	}
	return &oauth.OAuthToken{}, nil
}
func (provider *authFlowTestOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	provider.userInfoCalls++
	if provider.userInfoErr != nil {
		return nil, provider.userInfoErr
	}
	return &oauth.OAuthUser{ProviderUserID: "external-user"}, nil
}
func (*authFlowTestOAuthProvider) IsUserIDTaken(string) bool                         { return false }
func (*authFlowTestOAuthProvider) FillUserByProviderID(*identity.User, string) error { return nil }
func (*authFlowTestOAuthProvider) SetProviderUserID(*identity.User, string)          {}
func (*authFlowTestOAuthProvider) GetProviderPrefix() string                         { return "flow_" }
func (*authFlowTestOAuthProvider) ProviderUserIDColumn() string                      { return "" }

func setupAuthFlowControllerTest(t *testing.T) *authFlowTestOAuthProvider {
	t.Helper()
	previousDB := dbx.DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&identity.AuthFlow{}))
	dbx.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	provider := &authFlowTestOAuthProvider{}
	oauth.Register("auth-flow-test", provider)
	t.Cleanup(func() {
		oauth.Unregister("auth-flow-test")
		dbx.DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return provider
}

func TestGenerateOAuthCodeCarriesAffiliateInLoginFlow(t *testing.T) {
	setupAuthFlowControllerTest(t)
	c, recorder := fiberadapter.NewSyntheticContext(nil)
	fiberadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"auth-flow-test","intent":"login","aff":"invite-code"}`)))
	c.Headers().Set("Content-Type", "application/json")

	GenerateOAuthCode(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			FlowToken string `json:"flow_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	flow, err := identity.GetAuthFlow(response.Data.FlowToken, identity.AuthFlowMatch{
		Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentLogin,
	})
	require.NoError(t, err)
	var payload oauthFlowPayload
	require.NoError(t, common.UnmarshalJsonStr(flow.Payload, &payload))
	assert.Equal(t, "invite-code", payload.AffiliateCode)
	assert.Zero(t, flow.UserId)
	assert.Empty(t, flow.SessionId)
}

func TestGenerateOAuthCodeBindsFlowToAuthenticatedSession(t *testing.T) {
	setupAuthFlowControllerTest(t)
	c, recorder := fiberadapter.NewSyntheticContext(nil)
	fiberadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"auth-flow-test","intent":"bind"}`)))
	c.Headers().Set("Content-Type", "application/json")
	c.Set("id", 42)
	c.Set("session_id", "session-42")
	c.Set("auth_version", int64(3))
	c.Set("session_version", int64(2))

	GenerateOAuthCode(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			FlowToken string `json:"flow_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	flow, err := identity.GetAuthFlow(response.Data.FlowToken, identity.AuthFlowMatch{
		Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentBind,
		UserId: 42, SessionId: "session-42",
	})
	require.NoError(t, err)
	assert.Equal(t, 42, flow.UserId)
	assert.Equal(t, "session-42", flow.SessionId)
}

func TestOAuthLoginConsumesFlowOnlyAfterProviderIdentity(t *testing.T) {
	provider := setupAuthFlowControllerTest(t)

	tests := []struct {
		name        string
		exchangeErr error
		userInfoErr error
	}{
		{name: "exchange failure", exchangeErr: errors.New("exchange failed")},
		{name: "user info failure", userInfoErr: errors.New("user info failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider.exchangeErr = test.exchangeErr
			provider.userInfoErr = test.userInfoErr
			token, _, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
				Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentLogin,
				Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute),
			})
			require.NoError(t, err)

			testutil.ServeBufferedRoute(t, http.MethodGet, "/api/oauth/:provider", nil, HandleOAuth,
				httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+token+"&code=test", nil))

			flow, err := identity.GetAuthFlow(token, identity.AuthFlowMatch{
				Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentLogin,
			})
			require.NoError(t, err)
			assert.Nil(t, flow.ConsumedAt)
		})
	}
}

func TestOAuthLoginConsumesFlowAfterProviderIdentityAndOnProviderError(t *testing.T) {
	provider := setupAuthFlowControllerTest(t)

	provider.exchangeErr = nil
	provider.userInfoErr = nil
	successToken, _, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentLogin,
		Payload: `{invalid`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	testutil.ServeBufferedRoute(t, http.MethodGet, "/api/oauth/:provider", nil, HandleOAuth,
		httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+successToken+"&code=test", nil))
	_, err = identity.GetAuthFlow(successToken, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, identity.ErrAuthFlowConsumed)
	assert.Equal(t, 1, provider.exchangeCalls)
	assert.Equal(t, 1, provider.userInfoCalls)

	providerErrorToken, _, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentLogin,
		Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	testutil.ServeBufferedRoute(t, http.MethodGet, "/api/oauth/:provider", nil, HandleOAuth,
		httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+providerErrorToken+"&error=access_denied", nil))
	_, err = identity.GetAuthFlow(providerErrorToken, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, identity.ErrAuthFlowConsumed)
	assert.Equal(t, 1, provider.exchangeCalls)
	assert.Equal(t, 1, provider.userInfoCalls)
}

func TestOAuthBindProviderErrorConsumesSessionBoundFlow(t *testing.T) {
	provider := setupAuthFlowControllerTest(t)
	flowToken, _, err := identity.CreateAuthFlow(identity.AuthFlowCreate{
		Purpose: identity.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: identity.AuthFlowIntentBind,
		UserId: 42, SessionId: "session-42", Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	response := testutil.ServeBufferedRoute(t, http.MethodGet, "/api/oauth/:provider",
		[]contract.Middleware{func(c contract.Context) {
			c.Set("id", 42)
			c.Set("session_id", "session-42")
			c.Set("auth_version", int64(1))
			c.Set("session_version", int64(1))
			c.Next()
		}}, HandleOAuth,
		httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+flowToken+"&error=access_denied&error_description=cancelled", nil))

	assert.Equal(t, http.StatusOK, response.StatusCode)
	_, err = identity.GetAuthFlow(flowToken, identity.AuthFlowMatch{Purpose: identity.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, identity.ErrAuthFlowConsumed)
	assert.Zero(t, provider.exchangeCalls)
	assert.Zero(t, provider.userInfoCalls)
}
