package conformance

import (
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxKey is a private key type, so the SetContextValue case proves the value is
// stored under the caller's own key rather than a stringified copy of it.
type ctxKey string

func runRequestCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// ReadsRequestMetadata asserts the adapter surfaces the same request
	// metadata handlers read today, so migrating a handler off a framework
	// context cannot silently change routing or auth decisions.
	t.Run("ReadsRequestMetadata", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?model=gpt-4&empty=", nil)
		req.Header.Set("Authorization", "Bearer sk-test")
		req.Header.Set("Content-Type", "application/json")
		adapted, _ := adapter.NewContext(req)

		assert.Equal(t, http.MethodPost, adapted.Method())
		assert.Equal(t, "/v1/chat/completions", adapted.Path())
		assert.Equal(t, "gpt-4", adapted.Query("model"))
		assert.Equal(t, "", adapted.Query("empty"))
		assert.Equal(t, "fallback", adapted.DefaultQuery("absent", "fallback"))
		assert.Equal(t, "Bearer sk-test", adapted.Header("Authorization"))
		assert.Equal(t, "application/json", adapted.ContentType())
		assert.Equal(t, []string{"gpt-4"}, adapted.QueryValues()["model"])
	})

	// RequestTargetAccessors pins the accessors relay code uses to rebuild an
	// outbound request: a lost query string or a rewritten target silently
	// changes what the upstream provider receives.
	t.Run("RequestTargetAccessors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?model=gpt-4&empty=", strings.NewReader(`{}`))
		req.Header.Set("User-Agent", "newapi-test/1.0")
		adapted, _ := adapter.NewContext(req)

		assert.Equal(t, "/v1/chat/completions?model=gpt-4&empty=", adapted.RequestURI())
		assert.Equal(t, "model=gpt-4&empty=", adapted.RawQuery())
		assert.Equal(t, "newapi-test/1.0", adapted.UserAgent())
		assert.Equal(t, int64(2), adapted.ContentLength())
		assert.NotEmpty(t, adapted.ClientIP(), "client ip must be resolvable for rate limiting and abuse controls")
	})

	// HostReportsTheRequestAuthority pins the accessor the session-origin guard
	// and the OAuth redirect-URI builder compare against. Reporting a configured
	// or forwarded authority instead of the one the client addressed would either
	// break same-origin checks or send users to the wrong callback.
	t.Run("HostReportsTheRequestAuthority", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://panel.example.com:8443/api/user/auth/refresh", nil)
		req.Header.Set("X-Forwarded-Host", "attacker.example.com")
		adapted, _ := adapter.NewContext(req)

		assert.Equal(t, "panel.example.com:8443", adapted.Host(),
			"Host must report the authority the client addressed, including the port, and must not follow X-Forwarded-Host")
	})

	// IsTLSReflectsTheTransportNotForwardedHeaders is a security assertion, not a
	// convenience one: the session-origin guard derives the request scheme from
	// it, so an adapter that honoured X-Forwarded-Proto would let a client over
	// plaintext claim an https origin and pass the same-origin comparison.
	t.Run("IsTLSReflectsTheTransportNotForwardedHeaders", func(t *testing.T) {
		secure, _ := adapter.NewContext(httptest.NewRequest(http.MethodGet, "https://panel.example.com/api/status", nil))
		assert.True(t, secure.IsTLS(), "a request arriving over TLS must report IsTLS")

		spoofed := httptest.NewRequest(http.MethodGet, "http://panel.example.com/api/status", nil)
		spoofed.Header.Set("X-Forwarded-Proto", "https")
		spoofed.Header.Set("X-Forwarded-Ssl", "on")
		plaintext, _ := adapter.NewContext(spoofed)
		assert.False(t, plaintext.IsTLS(),
			"IsTLS must reflect the connection, never client-supplied forwarded headers")
	})

	// RouteParamsComeFromTheRouter asserts matched route parameters reach the
	// handler. Parameters are produced by the router, not by the request, so
	// this case must go through a real engine to mean anything.
	t.Run("RouteParamsComeFromTheRouter", func(t *testing.T) {
		var (
			single string
			all    map[string]string
			full   string
		)
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/v1/tasks/:task_id",
			Handler: func(c contract.Context) {
				single = c.Param("task_id")
				all = c.Params()
				full = c.FullPath()
			},
		}
		adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/v1/tasks/task-77", nil))

		assert.Equal(t, "task-77", single)
		assert.Equal(t, map[string]string{"task_id": "task-77"}, all)
		assert.Equal(t, "/v1/tasks/:task_id", full, "FullPath must report the route pattern, not the concrete path")
	})

	// HeadersExposeMultiValueEntries asserts the header view keeps every value
	// of a repeated header. Auth and forwarding code reads repeated headers,
	// and a view that collapsed them would drop credentials or proxy hops.
	t.Run("HeadersExposeMultiValueEntries", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Add("X-Forwarded-For", "203.0.113.1")
		req.Header.Add("X-Forwarded-For", "203.0.113.2")
		adapted, _ := adapter.NewContext(req)

		assert.Equal(t, []string{"203.0.113.1", "203.0.113.2"}, adapted.Headers().Values("X-Forwarded-For"))
		assert.Equal(t, "203.0.113.1", adapted.Header("X-Forwarded-For"),
			"single-value accessor must return the first value, matching net/http")
	})

	// HeaderMutationIsObservable asserts a write through Headers() is seen by
	// later readers. Auth middleware rewrites the inbound Authorization header
	// in place and downstream relay code reads it back, so the header view must
	// alias the request rather than hand out a copy.
	t.Run("HeaderMutationIsObservable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		req.Header.Set("x-goog-api-key", "goog-key")
		adapted, _ := adapter.NewContext(req)

		adapted.Headers().Set("Authorization", "Bearer rewritten")

		assert.Equal(t, "Bearer rewritten", adapted.Header("Authorization"))
		assert.Equal(t, "Bearer rewritten", adapted.Headers().Get("Authorization"))
		assert.Equal(t, "Bearer rewritten", adapted.HTTPRequest().Header.Get("Authorization"),
			"the header view must alias the request the escape hatch exposes")
	})

	// CookieReadsNamedCookie covers the session paths, which read the refresh
	// token from a cookie and must distinguish absent from empty.
	t.Run("CookieReadsNamedCookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
		req.AddCookie(&http.Cookie{Name: "newapi_refresh", Value: "refresh-token"})
		adapted, _ := adapter.NewContext(req)

		value, err := adapted.Cookie("newapi_refresh")
		require.NoError(t, err)
		assert.Equal(t, "refresh-token", value)

		_, missingErr := adapted.Cookie("absent")
		assert.Error(t, missingErr, "an absent cookie must report an error, not an empty value")
	})

	// FormValuesParseFromBody covers the OAuth and webhook endpoints that post
	// urlencoded bodies, including the repeated-field case.
	t.Run("FormValuesParseFromBody", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/oauth/token",
			strings.NewReader("grant_type=authorization_code&scope=read&scope=write"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		adapted, _ := adapter.NewContext(req)

		require.NoError(t, adapted.ParseForm())

		assert.Equal(t, "authorization_code", adapted.PostForm("grant_type"))
		assert.Equal(t, []string{"read", "write"}, adapted.PostFormValues()["scope"],
			"repeated form fields must keep every value")
	})

	// MultipartFormParsesFileAndFields covers the image-edit and task-submit
	// endpoints, which read both a file part and ordinary fields.
	t.Run("MultipartFormParsesFileAndFields", func(t *testing.T) {
		body := &strings.Builder{}
		writer := multipart.NewWriter(body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		filePart, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = filePart.Write([]byte{0x89, 0x50, 0x4E, 0x47})
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body.String()))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		adapted, _ := adapter.NewContext(req)

		form, err := adapted.MultipartForm()
		require.NoError(t, err)
		assert.Equal(t, []string{"gpt-image-1"}, form.Value["model"])
		require.Len(t, form.File["image"], 1)
		assert.Equal(t, "input.png", form.File["image"][0].Filename)
	})

	// RequestRewritesAreObservable asserts the protocol adapters that retarget
	// an inbound call (one vendor's verb and path onto another's) are seen by
	// downstream code reading through the contract.
	t.Run("RequestRewritesAreObservable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		adapted, _ := adapter.NewContext(req)

		adapted.SetPath("/v1/chat/completions")
		adapted.SetMethod(http.MethodPut)

		assert.Equal(t, "/v1/chat/completions", adapted.Path())
		assert.Equal(t, http.MethodPut, adapted.Method())
		assert.Equal(t, "/v1/chat/completions", adapted.HTTPRequest().URL.Path)
		assert.Equal(t, http.MethodPut, adapted.HTTPRequest().Method)
	})

	// ContextValueReachesRequestLifetime asserts a value attached to the
	// request context is readable through Context(), which is how code holding
	// only a context.Context (provider SDKs) observes per-request state.
	t.Run("ContextValueReachesRequestLifetime", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		adapted, _ := adapter.NewContext(req)

		adapted.SetContextValue(ctxKey("trace_id"), "trace-9")

		assert.Equal(t, "trace-9", adapted.Context().Value(ctxKey("trace_id")))
		assert.Nil(t, adapted.Context().Value(ctxKey("absent")))
	})

	// ContextIsCancelledWithTheRequest asserts the request lifetime is the
	// cancellation signal streaming code polls, rather than a background
	// context that never fires.
	t.Run("ContextIsCancelledWithTheRequest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx, cancel := context.WithCancel(req.Context())
		adapted, _ := adapter.NewContext(req.WithContext(ctx))

		require.NoError(t, adapted.Context().Err())
		cancel()
		assert.ErrorIs(t, adapted.Context().Err(), context.Canceled)
	})
}
