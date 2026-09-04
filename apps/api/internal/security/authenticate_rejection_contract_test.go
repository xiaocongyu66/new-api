package security

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenAuthRejectsMissingCredential pins the relay rejection envelope for an
// unauthenticated request. Relay clients (OpenAI SDKs) parse the nested `error`
// object, so its shape and the 401 status must survive the transport refactor.
func TestTokenAuthRejectsMissingCredential(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	response := testutil.ServeBufferedRoute(
		t, http.MethodPost, "/v1/chat/completions", []contract.Middleware{TokenAuth()},
		func(c contract.Context) { _ = c.JSON(http.StatusOK, common.H{"reached": true}) },
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil),
	)

	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Reached bool `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(readResponseBody(t, response), &body))

	assert.False(t, body.Reached, "handler must not run for a rejected request")
	assert.Equal(t, "new_api_error", body.Error.Type)
	assert.NotEmpty(t, body.Error.Message)
}

// TestTokenAuthRejectsMalformedBearerScheme pins that an empty bearer credential is
// rejected before the handler runs. The `sk-` prefix with no key reaches the
// actual Fiber route unchanged and becomes an empty credential without requiring
// token-table setup.
func TestTokenAuthRejectsMalformedBearerScheme(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk-")
	response := testutil.ServeBufferedRoute(
		t, http.MethodPost, "/v1/chat/completions", []contract.Middleware{TokenAuth()},
		func(c contract.Context) { _ = c.JSON(http.StatusOK, common.H{"reached": true}) }, request,
	)

	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
		Reached bool `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(readResponseBody(t, response), &body))

	assert.False(t, body.Reached)
	assert.Equal(t, "new_api_error", body.Error.Type)
}

// TestUserAuthRejectsAnonymousDashboardRequest pins the dashboard rejection
// envelope, which uses the flat success/message shape instead of the relay one.
func TestUserAuthRejectsAnonymousDashboardRequest(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)

	response := testutil.ServeBufferedRoute(
		t, http.MethodGet, "/api/user/self", []contract.Middleware{UserAuth()},
		func(c contract.Context) { _ = c.JSON(http.StatusOK, common.H{"reached": true}) },
		httptest.NewRequest(http.MethodGet, "/api/user/self", nil),
	)

	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Reached bool   `json:"reached"`
	}
	require.NoError(t, common.Unmarshal(readResponseBody(t, response), &body))

	assert.False(t, body.Reached)
	assert.False(t, body.Success)
	assert.NotEmpty(t, body.Message)
}

func readResponseBody(t *testing.T, response *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return body
}
