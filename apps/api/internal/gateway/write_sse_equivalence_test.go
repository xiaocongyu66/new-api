package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderSSEMatchesCustomEventFraming is the equivalence proof for moving
// RenderSSE off c.ResponseWriter() and onto contract.ResponseStream.
//
// It renders every payload shape the relay emits through both paths: the
// framing primitive on its own (CustomEvent.RenderTo onto a plain buffer) and
// the migrated one through the contract, then compares the bytes and the
// headers. The shapes matter individually because CustomEvent is not a uniform
// framer: it escapes CR, and appends the blank-line terminator only when the
// payload starts with "data", so `event:` prefixes and partial frames pass
// through unterminated. Routing these through EventStream.WriteEvent instead
// would terminate and re-prefix all of them, which is why RenderSSE stayed on
// this path.
//
// The header half is asserted against the literal SSE values the deleted
// CustomEvent.WriteContentType wrote ("text/event-stream", and "no-cache" when
// nothing set Cache-Control); its conditional branch is pinned separately by
// TestRenderSSEPreservesAnExistingCacheControl below.
func TestRenderSSEMatchesCustomEventFraming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "data frame", payload: `data: {"choices":[{"delta":{"content":"hi"}}]}`},
		{name: "terminal sentinel", payload: "data: [DONE]"},
		{name: "named event prefix", payload: "event: message_start\n"},
		{name: "claude data line", payload: "data: {\"type\":\"content_block_delta\"}\n"},
		{name: "payload carrying CR", payload: "data: {\"text\":\"a\rb\"}"},
		{name: "payload carrying LF", payload: "data: {\"text\":\"a\nb\"}"},
		{name: "empty data", payload: "data: "},
		{name: "unprefixed payload stays unterminated", payload: "ping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Framing reference: the writer-neutral primitive RenderSSE builds on.
			var framed bytes.Buffer
			require.NoError(t, common.CustomEvent{Data: tc.payload}.RenderTo(&framed))

			// Migrated path, through contract.ResponseStream.
			migrated := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(migrated)
			ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			RenderSSE(ginadapter.Wrap(ginCtx), tc.payload)

			assert.Equal(t, framed.String(), migrated.Body.String(),
				"SSE bytes must be identical to the CustomEvent framing primitive")
			assert.Equal(t, "text/event-stream", migrated.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache", migrated.Header().Get("Cache-Control"))
		})
	}
}

// TestRenderSSEPreservesAnExistingCacheControl pins the conditional half of the
// header write. CustomEvent installs Cache-Control only when nothing set it, so
// a route that already applied a stricter no-store policy keeps it. Replacing
// that read with an unconditional Set would silently downgrade those responses.
func TestRenderSSEPreservesAnExistingCacheControl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const stricter = "no-store, no-cache, must-revalidate, private, max-age=0"

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	adapted := ginadapter.Wrap(ginCtx)
	adapted.SetHeader("Cache-Control", stricter)

	RenderSSE(adapted, "data: {}")

	assert.Equal(t, stricter, recorder.Header().Get("Cache-Control"),
		"an already-set Cache-Control must survive the SSE header write")
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

// unflushableWriter is an http.ResponseWriter with no Flush method, standing in
// for a writer the transport cannot push through (a wrapping middleware that
// buffers, or a recorder in a synthetic pipeline).
type unflushableWriter struct {
	header http.Header
	body   []byte
	status int
}

func (w *unflushableWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *unflushableWriter) Write(payload []byte) (int, error) {
	w.body = append(w.body, payload...)
	return len(payload), nil
}

func (w *unflushableWriter) WriteHeader(status int) { w.status = status }

// TestFlushFallbackStaysObservableWithoutFlushSupport is the flush-fallback
// proof. CanFlush exists so a writer that cannot flush is a reported condition
// rather than a stream that quietly buffers to completion and looks to an
// operator like a stalled provider.
//
// It asserts three things on such a writer: the capability probe says no, the
// flush attempt returns an error rather than panicking or silently succeeding,
// and the payload bytes still reach the writer so the response is correct even
// though it is not incremental.
func TestFlushFallbackStaysObservableWithoutFlushSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	writer := &unflushableWriter{}
	ginCtx, _ := gin.CreateTestContext(writer)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	adapted := ginadapter.Wrap(ginCtx)

	stream := adapted.ResponseStream()
	require.NotNil(t, stream)

	assert.False(t, stream.CanFlush(),
		"a writer with no Flush method must report that it cannot flush")
	assert.Error(t, stream.Flush(),
		"flushing an unflushable writer must surface an error, not succeed silently")
	assert.Error(t, FlushWriter(adapted),
		"the relay flush helper must report the missing flusher rather than degrade silently")

	RenderSSE(adapted, "data: {}")
	assert.Equal(t, "data: {}\n\n", string(writer.body),
		"the frame must still be written even when the writer cannot flush")

	assert.False(t, stream.SetWriteDeadline(time.Now().Add(time.Second)),
		"a writer with no connection underneath must report the deadline as unsupported")
}

// TestFlushSupportedWriterReportsAndFlushes is the positive half: on a real
// flushable writer the same probe says yes and the flush reaches the client, so
// the fallback above is a genuine capability difference and not a probe that
// always reports false.
func TestFlushSupportedWriterReportsAndFlushes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	adapted := ginadapter.Wrap(ginCtx)

	stream := adapted.ResponseStream()
	require.NotNil(t, stream)

	assert.True(t, stream.CanFlush())
	require.NoError(t, stream.Flush())
	require.NoError(t, FlushWriter(adapted))
	assert.True(t, recorder.Flushed, "a flushable writer must actually be flushed")
}
