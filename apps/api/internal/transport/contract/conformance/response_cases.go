package conformance

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runResponseCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// JSONMatchesEnvelope asserts the adapter emits the exact bytes and status
	// the current handlers emit.
	t.Run("JSONMatchesEnvelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		adapted, recorder := adapter.NewContext(req)

		require.NoError(t, adapted.JSON(http.StatusOK, map[string]any{
			"success": true,
			"message": "",
			"data":    map[string]any{"id": 7},
		}))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
		assert.JSONEq(t, `{"success":true,"message":"","data":{"id":7}}`, recorder.Body.String())
	})

	// DataWritesVerbatimBytes covers the endpoints that return a non-JSON
	// payload built upstream (images, exported files): the bytes must arrive
	// unmodified under the caller's content type.
	t.Run("DataWritesVerbatimBytes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/image/1", nil)
		adapted, recorder := adapter.NewContext(req)

		payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}
		require.NoError(t, adapted.Data(http.StatusOK, "image/png", payload))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
		assert.Equal(t, payload, recorder.Body.Bytes())
	})

	// StringWritesPlainBody covers the health and text endpoints.
	t.Run("StringWritesPlainBody", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		adapted, recorder := adapter.NewContext(req)

		require.NoError(t, adapted.String(http.StatusOK, "pong"))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "pong", recorder.Body.String())
	})

	// RedirectSetsLocationAndStatus covers the OAuth callbacks, where a wrong
	// status or a missing Location breaks the browser flow.
	t.Run("RedirectSetsLocationAndStatus", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/github", nil)
		adapted, recorder := adapter.NewContext(req)

		adapted.Redirect(http.StatusFound, "https://example.test/callback?code=1")

		assert.Equal(t, http.StatusFound, recorder.Code)
		assert.Equal(t, "https://example.test/callback?code=1", recorder.Header().Get("Location"))
	})

	// StatusAndResponseStatusAgree asserts middleware can read back the status
	// after the handler ran. Logging and billing middleware branch on it, and a
	// zero reading would misclassify every response.
	t.Run("StatusAndResponseStatusAgree", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		adapted, recorder := adapter.NewContext(req)

		adapted.Status(http.StatusAccepted)

		assert.Equal(t, http.StatusAccepted, adapted.ResponseStatus())
		require.NoError(t, adapted.String(http.StatusAccepted, ""))
		assert.Equal(t, http.StatusAccepted, recorder.Code)
	})

	// SetHeaderReachesTheClient covers the headers business code adds around a
	// response (request id, rate-limit counters).
	t.Run("SetHeaderReachesTheClient", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		adapted, recorder := adapter.NewContext(req)

		adapted.SetHeader("X-Request-Id", "req-42")
		require.NoError(t, adapted.JSON(http.StatusOK, map[string]any{"success": true}))

		assert.Equal(t, "req-42", recorder.Header().Get("X-Request-Id"))
	})

	// SetCookieEmitsAttributes covers the session cookies. The attributes are
	// the security boundary: losing HttpOnly or Secure would expose the refresh
	// token to scripts or to plaintext transport.
	t.Run("SetCookieEmitsAttributes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/user/login", nil)
		adapted, recorder := adapter.NewContext(req)

		adapted.SetCookie(&http.Cookie{
			Name:     "newapi_refresh",
			Value:    "refresh-token",
			Path:     "/",
			MaxAge:   3600,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		setCookie := recorder.Header().Get("Set-Cookie")
		assert.Contains(t, setCookie, "newapi_refresh=refresh-token")
		assert.Contains(t, setCookie, "Path=/")
		assert.Contains(t, setCookie, "Max-Age=3600")
		assert.Contains(t, setCookie, "HttpOnly")
		assert.Contains(t, setCookie, "Secure")
		assert.Contains(t, setCookie, "SameSite=Lax")
	})

	// CaptureResponseMirrorsBodyWithoutConsumingIt asserts audit middleware can
	// inspect what the handler wrote while the client still receives it. A
	// capture that swallowed the body would blank out every audited response.
	t.Run("CaptureResponseMirrorsBodyWithoutConsumingIt", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/channel", nil)
		adapted, recorder := adapter.NewContext(req)

		capture := adapted.CaptureResponse(64 * 1024)
		require.NotNil(t, capture, "the adapter must be able to intercept the response for audit middleware")

		require.NoError(t, adapted.JSON(http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 3}}))

		assert.JSONEq(t, `{"success":true,"data":{"id":3}}`, string(capture.Body()))
		assert.JSONEq(t, `{"success":true,"data":{"id":3}}`, recorder.Body.String(),
			"capturing must not consume the bytes the client receives")
	})

	// CaptureResponseTruncatesAtLimit asserts the capture is bounded, so a large
	// response cannot be buffered without limit, while the client still gets the
	// whole body.
	t.Run("CaptureResponseTruncatesAtLimit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/channel", nil)
		adapted, recorder := adapter.NewContext(req)

		capture := adapted.CaptureResponse(8)
		require.NotNil(t, capture)

		require.NoError(t, adapted.String(http.StatusOK, "0123456789abcdef"))

		assert.Equal(t, "01234567", string(capture.Body()), "capture must stop at the configured limit")
		assert.Equal(t, "0123456789abcdef", recorder.Body.String(),
			"truncating the capture must not truncate the response")
	})
}
