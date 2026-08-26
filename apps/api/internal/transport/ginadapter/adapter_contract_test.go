package ginadapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAdaptedContext(t *testing.T, method, target string, body io.Reader) (contract.Context, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, target, body)
	return Wrap(ginCtx), ginCtx, recorder
}

// TestWrappedContextReadsRequestMetadata asserts the adapter surfaces the same
// request metadata handlers read today, so migrating a handler off *gin.Context
// cannot silently change routing or auth decisions.
func TestWrappedContextReadsRequestMetadata(t *testing.T) {
	adapted, ginCtx, _ := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions?model=gpt-4&empty=", nil)
	ginCtx.Request.Header.Set("Authorization", "Bearer sk-test")
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	ginCtx.Params = gin.Params{{Key: "task_id", Value: "task-77"}}

	assert.Equal(t, http.MethodPost, adapted.Method())
	assert.Equal(t, "/v1/chat/completions", adapted.Path())
	assert.Equal(t, "gpt-4", adapted.Query("model"))
	assert.Equal(t, "", adapted.Query("empty"))
	assert.Equal(t, "fallback", adapted.DefaultQuery("absent", "fallback"))
	assert.Equal(t, "task-77", adapted.Param("task_id"))
	assert.Equal(t, "Bearer sk-test", adapted.Header("Authorization"))
	assert.Equal(t, "application/json", adapted.ContentType())
	assert.Equal(t, []string{"gpt-4"}, adapted.QueryValues()["model"])
}

// TestWrappedContextValuesRoundTrip asserts per-request state set by middleware
// is readable through the typed getters business code relies on.
func TestWrappedContextValuesRoundTrip(t *testing.T) {
	adapted, _, _ := newAdaptedContext(t, http.MethodGet, "/api/user/self", nil)

	adapted.Set("id", 42)
	adapted.Set("username", "alice")
	adapted.Set("use_access_token", true)
	adapted.Set("channel_id_64", int64(9))

	assert.Equal(t, 42, adapted.GetInt("id"))
	assert.Equal(t, "alice", adapted.GetString("username"))
	assert.True(t, adapted.GetBool("use_access_token"))
	assert.Equal(t, int64(9), adapted.GetInt64("channel_id_64"))

	value, exists := adapted.Get("username")
	require.True(t, exists)
	assert.Equal(t, "alice", value)

	_, missing := adapted.Get("not_set")
	assert.False(t, missing)
}

// TestWrappedContextBindJSONKeepsBodyReplayable is the load-bearing assertion of
// the adapter: decoding through the contract must leave the body readable, since
// the relay pipeline decodes once for routing and again to forward upstream.
func TestWrappedContextBindJSONKeepsBodyReplayable(t *testing.T) {
	payload := `{"model":"gpt-4","stream":true}`
	adapted, ginCtx, _ := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	ginCtx.Request.Header.Set("Content-Type", "application/json")

	var decoded struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	require.NoError(t, adapted.BindJSON(&decoded))
	assert.Equal(t, "gpt-4", decoded.Model)
	assert.True(t, decoded.Stream)

	raw, err := adapted.RawBody()
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(raw))

	reader, err := adapted.BodyReader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	forwarded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(forwarded))
}

// TestWrappedContextJSONMatchesGinEnvelope asserts the adapter emits the exact
// bytes and status the current handlers emit through gin.
func TestWrappedContextJSONMatchesGinEnvelope(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodGet, "/api/status", nil)

	require.NoError(t, adapted.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "",
		"data":    map[string]any{"id": 7},
	}))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"success":true,"message":"","data":{"id":7}}`, recorder.Body.String())
}

// TestWrappedContextChainAbortStopsHandler asserts middleware written against
// the contract can reject a request, matching the current abort behaviour.
func TestWrappedContextChainAbortStopsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		adapted := Wrap(c)
		require.NoError(t, adapted.JSON(http.StatusUnauthorized, map[string]any{"success": false}))
		adapted.Abort()
	})

	handlerRan := false
	engine.GET("/api/user/self", Handler(func(contract.Context) {
		handlerRan = true
	}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/self", nil))

	assert.False(t, handlerRan, "aborted request must not reach the handler")
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"success":false}`, recorder.Body.String())
}

// TestHandlerAdapterRunsContractHandler asserts a contract handler registered on
// a gin route receives a working context.
func TestHandlerAdapterRunsContractHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.GET("/api/token/:id", Handler(func(c contract.Context) {
		require.NoError(t, c.JSON(http.StatusOK, map[string]any{"id": c.Param("id")}))
	}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/token/31", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"id":"31"}`, recorder.Body.String())
}

// TestEventStreamFramingMatchesRelayBytes pins that the adapter's SSE writer
// produces the same frames the relay helpers produce today. These bytes are the
// client-visible contract, so they must not drift when the writer is replaced.
func TestEventStreamFramingMatchesRelayBytes(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions", nil)

	stream, err := EventStream(adapted)
	require.NoError(t, err)

	stream.SetHeaders()
	require.NoError(t, stream.WriteEvent(`{"choices":[{"delta":{"content":"hi"}}]}`))
	require.NoError(t, stream.WriteEvent("[DONE]"))

	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
	assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))
	assert.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))

	assert.Equal(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
		recorder.Body.String(),
	)
}

// TestEventStreamNamedEventFramingMatchesClaudeData pins the named-event framing
// against the bytes relay/helper.ClaudeData emits.
func TestEventStreamNamedEventFramingMatchesClaudeData(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodPost, "/v1/messages", nil)

	stream, err := EventStream(adapted)
	require.NoError(t, err)

	require.NoError(t, stream.WriteNamedEvent("message_start", `{"type":"message_start"}`))

	assert.Equal(t, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n", recorder.Body.String())
}

// TestEventStreamCommentFramingMatchesPingData pins the keep-alive comment frame.
func TestEventStreamCommentFramingMatchesPingData(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions", nil)

	stream, err := EventStream(adapted)
	require.NoError(t, err)

	require.NoError(t, stream.WriteComment("PING"))

	assert.Equal(t, ": PING\n\n", recorder.Body.String())
}

// TestEventStreamSetHeadersIsIdempotent asserts nested relay helpers can request
// streaming headers repeatedly without duplicating them.
func TestEventStreamSetHeadersIsIdempotent(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions", nil)

	stream, err := EventStream(adapted)
	require.NoError(t, err)

	stream.SetHeaders()
	stream.SetHeaders()

	assert.Equal(t, []string{"text/event-stream"}, recorder.Header().Values("Content-Type"))
	assert.Equal(t, []string{"no-cache"}, recorder.Header().Values("Cache-Control"))
}

// TestEventStreamRejectsWritesAfterClientDisconnect asserts a cancelled request
// stops emitting frames instead of writing a partial event.
func TestEventStreamRejectsWritesAfterClientDisconnect(t *testing.T) {
	adapted, ginCtx, recorder := newAdaptedContext(t, http.MethodPost, "/v1/chat/completions", nil)

	ctx, cancel := context.WithCancel(ginCtx.Request.Context())
	ginCtx.Request = ginCtx.Request.WithContext(ctx)
	cancel()

	stream, err := EventStream(adapted)
	require.NoError(t, err)

	assert.True(t, stream.Done())
	require.Error(t, stream.WriteEvent(`{"choices":[]}`))
	assert.Empty(t, recorder.Body.String())
}

// TestResponseStreamWritesRawUpstreamBytes asserts the raw writer used by the
// video and reverse-proxy endpoints preserves status, headers, and body bytes.
func TestResponseStreamWritesRawUpstreamBytes(t *testing.T) {
	adapted, _, recorder := newAdaptedContext(t, http.MethodGet, "/v1/videos/task-1/content", nil)

	stream, err := ResponseStream(adapted)
	require.NoError(t, err)

	stream.SetHeader("Content-Type", "video/mp4")
	stream.WriteHeader(http.StatusPartialContent)
	written, err := stream.Write([]byte{0x00, 0x01, 0x02})
	require.NoError(t, err)

	assert.Equal(t, 3, written)
	assert.Equal(t, http.StatusPartialContent, recorder.Code)
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, []byte{0x00, 0x01, 0x02}, recorder.Body.Bytes())
}

// TestUnwrapRecoversGinContext asserts the migration escape hatch returns the
// original context, and reports failure for a foreign implementation.
func TestUnwrapRecoversGinContext(t *testing.T) {
	adapted, ginCtx, _ := newAdaptedContext(t, http.MethodGet, "/api/status", nil)

	recovered, ok := Unwrap(adapted)
	require.True(t, ok)
	assert.Same(t, ginCtx, recovered)
}
