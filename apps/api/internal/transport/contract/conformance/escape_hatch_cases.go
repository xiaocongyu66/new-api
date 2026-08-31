package conformance

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runEscapeHatchCases covers HTTPRequest() and ResponseWriter(), the two
// standard-library escape hatches third-party libraries consume (WebAuthn, OAuth
// exchanges, WebSocket upgrades, reverse proxies).
//
// A framework that cannot synthesize a faithful *http.Request or
// http.ResponseWriter breaks those libraries in ways no contract-level accessor
// test would catch, so round-trip fidelity is asserted directly.
func runEscapeHatchCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// HTTPRequestPreservesRequestLine asserts the synthesized request carries
	// the same target, method, and host the contract accessors report.
	t.Run("HTTPRequestPreservesRequestLine", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "https://api.test/v1/chat/completions?model=gpt-4&empty=", nil)
		adapted, _ := adapter.NewContext(req)

		exposed := adapted.HTTPRequest()
		require.NotNil(t, exposed, "third-party libraries require a real *http.Request")

		assert.Equal(t, adapted.Method(), exposed.Method)
		assert.Equal(t, adapted.Path(), exposed.URL.Path)
		assert.Equal(t, adapted.RawQuery(), exposed.URL.RawQuery)
		assert.Equal(t, adapted.RequestURI(), exposed.RequestURI)
		assert.Equal(t, "api.test", exposed.Host)
	})

	// HTTPRequestPreservesMultiValueHeaders asserts every header value reaches
	// the standard-library request. OAuth and proxy libraries read repeated
	// headers straight off it.
	t.Run("HTTPRequestPreservesMultiValueHeaders", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		req.Header.Set("Authorization", "Bearer sk-test")
		req.Header.Add("X-Forwarded-For", "203.0.113.1")
		req.Header.Add("X-Forwarded-For", "203.0.113.2")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Accept", "text/event-stream")
		adapted, _ := adapter.NewContext(req)

		exposed := adapted.HTTPRequest()

		assert.Equal(t, "Bearer sk-test", exposed.Header.Get("Authorization"))
		assert.Equal(t, []string{"203.0.113.1", "203.0.113.2"}, exposed.Header.Values("X-Forwarded-For"))
		assert.Equal(t, []string{"application/json", "text/event-stream"}, exposed.Header.Values("Accept"))
		assert.Equal(t, adapted.Headers().Values("X-Forwarded-For"), exposed.Header.Values("X-Forwarded-For"),
			"the contract header view and the escape hatch must report the same values")
	})

	// HTTPRequestPreservesCookies asserts cookies survive onto the synthesized
	// request, which is how the session libraries read them.
	t.Run("HTTPRequestPreservesCookies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
		req.AddCookie(&http.Cookie{Name: "newapi_refresh", Value: "refresh-token"})
		req.AddCookie(&http.Cookie{Name: "newapi_session", Value: "sid-9"})
		adapted, _ := adapter.NewContext(req)

		exposed := adapted.HTTPRequest()

		refresh, err := exposed.Cookie("newapi_refresh")
		require.NoError(t, err)
		assert.Equal(t, "refresh-token", refresh.Value)

		session, err := exposed.Cookie("newapi_session")
		require.NoError(t, err)
		assert.Equal(t, "sid-9", session.Value)
	})

	// HTTPRequestFormRoundTripsThroughStdlib asserts a urlencoded body can be
	// parsed by the standard library off the exposed request, including repeated
	// fields, and that the contract accessors agree with it.
	t.Run("HTTPRequestFormRoundTripsThroughStdlib", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/oauth/token?state=xyz",
			strings.NewReader("grant_type=authorization_code&code=abc123&scope=read&scope=write"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		adapted, _ := adapter.NewContext(req)

		exposed := adapted.HTTPRequest()
		require.NoError(t, exposed.ParseForm())

		assert.Equal(t, "authorization_code", exposed.PostFormValue("grant_type"))
		assert.Equal(t, "abc123", exposed.PostFormValue("code"))
		assert.Equal(t, []string{"read", "write"}, exposed.PostForm["scope"],
			"repeated form fields must survive onto the standard-library request")
		assert.Equal(t, "xyz", exposed.Form.Get("state"),
			"ParseForm must merge the query string, matching net/http")
		assert.Equal(t, exposed.PostForm["scope"], adapted.PostFormValues()["scope"],
			"the contract form view and the escape hatch must agree")
	})

	// HTTPRequestBodyBufferingIsIdempotent asserts buffering through the
	// contract consumes the inbound stream exactly once and then serves every
	// later read from the buffer. Middleware, billing, and routing each read the
	// body independently, so a second buffering read that re-drained the socket
	// would hand back an empty payload.
	//
	// Note what is deliberately NOT asserted: buffering does not rewind
	// HTTPRequest().Body. Only the decoding entry points (BindJSON,
	// MultipartForm) reinstall a rewound body, and relay forwarding reads
	// through BodyReader rather than off the raw request. Requiring a rewind
	// here would invent a guarantee no caller relies on.
	t.Run("HTTPRequestBodyBufferingIsIdempotent", func(t *testing.T) {
		payload := `{"model":"gpt-4","stream":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _ := adapter.NewContext(req)

		// Buffer through the contract first, the way middleware does.
		raw, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(raw))

		// Every later read, in either style, must still see the whole payload.
		afterBuffer, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(afterBuffer),
			"a second buffering read must be served from the buffer, not from the drained stream")

		reader, err := adapted.BodyReader()
		require.NoError(t, err)
		t.Cleanup(func() { _ = reader.Close() })
		forwarded, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(forwarded),
			"the forwarding reader must see the buffered payload after the escape hatch was exposed")
	})

	// BindJSONRewindsTheStdlibBody asserts the decoding entry point reinstalls a
	// readable body on the standard-library request. This is the guarantee the
	// relay pipeline depends on: it decodes once for routing, then hands the
	// request to code that reads HTTPRequest().Body directly.
	t.Run("BindJSONRewindsTheStdlibBody", func(t *testing.T) {
		payload := `{"model":"gpt-4","stream":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _ := adapter.NewContext(req)

		var decoded struct {
			Model string `json:"model"`
		}
		require.NoError(t, adapted.BindJSON(&decoded))
		assert.Equal(t, "gpt-4", decoded.Model)

		forwarded, err := io.ReadAll(adapted.HTTPRequest().Body)
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(forwarded),
			"decoding through the contract must leave the standard-library body readable")
	})

	// HTTPRequestMultipartRoundTripsThroughStdlib asserts a multipart body can
	// be parsed off the exposed request after the contract already parsed it,
	// which is exactly what the image-edit path does: it calls MultipartForm and
	// then reads HTTPRequest().MultipartForm.
	t.Run("HTTPRequestMultipartRoundTripsThroughStdlib", func(t *testing.T) {
		body := &strings.Builder{}
		writer := multipart.NewWriter(body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("n", "2"))
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
		assert.Equal(t, []string{"2"}, form.Value["n"])
		require.Len(t, form.File["image"], 1)

		file, err := form.File["image"][0].Open()
		require.NoError(t, err)
		t.Cleanup(func() { _ = file.Close() })
		contents, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, []byte{0x89, 0x50, 0x4E, 0x47}, contents,
			"the uploaded bytes must survive the replayable-body path")
	})

	// ResponseWriterWritesThroughToTheClient asserts bytes and headers written
	// on the exposed writer reach the client, the way the reverse proxy and the
	// WebSocket upgrade write them.
	t.Run("ResponseWriterWritesThroughToTheClient", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
		adapted, recorder := adapter.NewContext(req)

		writer := adapted.ResponseWriter()
		require.NotNil(t, writer, "libraries that take over the response require a real http.ResponseWriter")

		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Add("X-Upstream", "provider-a")
		writer.Header().Add("X-Upstream", "provider-b")
		writer.WriteHeader(http.StatusPartialContent)
		written, err := writer.Write([]byte{0x00, 0x01, 0x02})
		require.NoError(t, err)

		assert.Equal(t, 3, written)
		assert.Equal(t, http.StatusPartialContent, recorder.Code)
		assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
		assert.Equal(t, []string{"provider-a", "provider-b"}, recorder.Header().Values("X-Upstream"),
			"multi-value response headers must survive the escape hatch")
		assert.Equal(t, []byte{0x00, 0x01, 0x02}, recorder.Body.Bytes())
	})

	// ResponseWriterSharesStateWithTheContract asserts the exposed writer is the
	// same response the contract writes to, not a detached one. Middleware sets
	// headers through the contract while a library writes the body through the
	// hatch, and both must land on one response.
	t.Run("ResponseWriterSharesStateWithTheContract", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
		adapted, recorder := adapter.NewContext(req)

		adapted.SetHeader("X-Request-Id", "req-42")
		writer := adapted.ResponseWriter()

		assert.Equal(t, "req-42", writer.Header().Get("X-Request-Id"),
			"a header set through the contract must be visible on the exposed writer")

		writer.Header().Set("X-Upstream-Status", "200")
		assert.Equal(t, "200", recorder.Header().Get("X-Upstream-Status"),
			"a header set on the exposed writer must reach the client")

		writer.WriteHeader(http.StatusOK)
		assert.Equal(t, http.StatusOK, adapted.ResponseStatus(),
			"a status written through the hatch must be readable through the contract")
	})

	// ResponseWriterSupportsFlush asserts the exposed writer is flushable, which
	// streaming libraries require to push bytes incrementally instead of
	// buffering a whole response.
	t.Run("ResponseWriterSupportsFlush", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		adapted, recorder := adapter.NewContext(req)

		writer := adapted.ResponseWriter()
		flusher, ok := writer.(http.Flusher)
		require.True(t, ok, "streaming libraries require the exposed writer to implement http.Flusher")

		_, err := writer.Write([]byte("chunk-1"))
		require.NoError(t, err)
		flusher.Flush()

		assert.Equal(t, "chunk-1", recorder.Body.String())
		assert.True(t, recorder.Flushed, "Flush must reach the underlying response")
	})
}
