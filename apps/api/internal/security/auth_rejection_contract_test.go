package security

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenAuthRejectsMissingCredential pins the relay rejection envelope for an
// unauthenticated request. Relay clients (OpenAI SDKs) parse the nested `error`
// object, so its shape and the 401 status must survive the transport refactor.
func TestTokenAuthRejectsMissingCredential(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	engine := gin.New()
	engine.Use(ginadapter.Middleware(TokenAuth()))
	engine.POST("/v1/chat/completions", ginadapter.Handler(func(c contract.Context) {
		_ = c.JSON(http.StatusOK, common.H{"reached": true})
	}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Reached bool `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))

	assert.False(t, body.Reached, "handler must not run for a rejected request")
	assert.Equal(t, "new_api_error", body.Error.Type)
	assert.NotEmpty(t, body.Error.Message)
}

// TestTokenAuthRejectsMalformedBearerScheme pins that a malformed credential is
// rejected before the handler runs. A blank bearer value never reaches the token
// lookup, so this covers the reject path without requiring the token table and
// dialect-specific column initialisation that model package tests own.
func TestTokenAuthRejectsMalformedBearerScheme(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	engine := gin.New()
	engine.Use(ginadapter.Middleware(TokenAuth()))
	engine.POST("/v1/chat/completions", ginadapter.Handler(func(c contract.Context) {
		_ = c.JSON(http.StatusOK, common.H{"reached": true})
	}))

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer    ")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
		Reached bool `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))

	assert.False(t, body.Reached)
	assert.Equal(t, "new_api_error", body.Error.Type)
}

// TestUserAuthRejectsAnonymousDashboardRequest pins the dashboard rejection
// envelope, which uses the flat success/message shape instead of the relay one.
func TestUserAuthRejectsAnonymousDashboardRequest(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	engine := gin.New()
	engine.Use(ginadapter.Middleware(UserAuth()))
	engine.GET("/api/user/self", ginadapter.Handler(func(c contract.Context) {
		_ = c.JSON(http.StatusOK, common.H{"reached": true})
	}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/self", nil))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Reached bool   `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))

	assert.False(t, body.Reached)
	assert.False(t, body.Success)
	assert.NotEmpty(t, body.Message)
}
