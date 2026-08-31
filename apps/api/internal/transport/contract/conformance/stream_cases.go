package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runEventStreamCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// FramingMatchesRelayBytes pins that the adapter's SSE writer produces the
	// same frames the relay helpers produce today. These bytes are the
	// client-visible contract, so they must not drift when the writer is
	// replaced.
	t.Run("FramingMatchesRelayBytes", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
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
	})

	// NamedEventFramingMatchesClaudeData pins the named-event framing against
	// the bytes relay/helper.ClaudeData emits.
	t.Run("NamedEventFramingMatchesClaudeData", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/messages", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		require.NoError(t, stream.WriteNamedEvent("message_start", `{"type":"message_start"}`))

		assert.Equal(t, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n", recorder.Body.String())
	})

	// CommentFramingMatchesPingData pins the keep-alive comment frame.
	t.Run("CommentFramingMatchesPingData", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		require.NoError(t, stream.WriteComment("PING"))

		assert.Equal(t, ": PING\n\n", recorder.Body.String())
	})

	// SetHeadersIsIdempotent asserts nested relay helpers can request streaming
	// headers repeatedly without duplicating them.
	t.Run("SetHeadersIsIdempotent", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		stream.SetHeaders()
		stream.SetHeaders()

		assert.Equal(t, []string{"text/event-stream"}, recorder.Header().Values("Content-Type"))
		assert.Equal(t, []string{"no-cache"}, recorder.Header().Values("Cache-Control"))
	})

	// WriteRawEmitsBytesVerbatim covers the path that forwards already-framed
	// upstream bytes. Any added framing would corrupt the stream clients parse.
	t.Run("WriteRawEmitsBytesVerbatim", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		payload := []byte("data: {\"passthrough\":true}\n\n")
		written, err := stream.WriteRaw(payload)
		require.NoError(t, err)

		assert.Equal(t, len(payload), written)
		assert.Equal(t, string(payload), recorder.Body.String())
	})

	// FlushReachesTheClient asserts frames are pushed rather than buffered until
	// the handler returns, which is what makes the stream incremental.
	t.Run("FlushReachesTheClient", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		require.NoError(t, stream.WriteEvent(`{"delta":"a"}`))
		require.NoError(t, stream.Flush())

		assert.True(t, recorder.Flushed, "SSE writes must be flushed to the client")
	})

	// DoneIsFalseOnALiveRequest asserts the disconnect probe does not report a
	// live client as gone, which would truncate every stream.
	t.Run("DoneIsFalseOnALiveRequest", func(t *testing.T) {
		adapted, _ := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		assert.False(t, stream.Done())
	})

	// RejectsWritesAfterClientDisconnect asserts a cancelled request stops
	// emitting frames instead of writing a partial event.
	t.Run("RejectsWritesAfterClientDisconnect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx, cancel := context.WithCancel(req.Context())
		adapted, recorder := adapter.NewContext(req.WithContext(ctx))
		cancel()

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		assert.True(t, stream.Done())
		require.Error(t, stream.WriteEvent(`{"choices":[]}`))
		assert.Empty(t, recorder.Body.String())
	})

	// EveryWriteRejectsAfterDisconnect asserts the whole writer surface refuses
	// a gone client, not only WriteEvent. A single unguarded method would keep
	// the relay pulling from the provider after the client left, which is billed
	// traffic nobody receives.
	t.Run("EveryWriteRejectsAfterDisconnect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		ctx, cancel := context.WithCancel(req.Context())
		adapted, recorder := adapter.NewContext(req.WithContext(ctx))
		cancel()

		stream, err := adapter.NewEventStream(adapted)
		require.NoError(t, err)

		require.Error(t, stream.WriteNamedEvent("message_start", `{"type":"message_start"}`))
		require.Error(t, stream.WriteComment("PING"))
		_, rawErr := stream.WriteRaw([]byte("data: x\n\n"))
		require.Error(t, rawErr)
		require.Error(t, stream.Flush())

		assert.Empty(t, recorder.Body.String(), "a disconnected client must receive no partial frames")
	})
}

func runResponseStreamCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// WritesRawUpstreamBytes asserts the raw writer used by the video and
	// reverse-proxy endpoints preserves status, headers, and body bytes.
	t.Run("WritesRawUpstreamBytes", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		stream.SetHeader("Content-Type", "video/mp4")
		stream.WriteHeader(http.StatusPartialContent)
		written, err := stream.Write([]byte{0x00, 0x01, 0x02})
		require.NoError(t, err)

		assert.Equal(t, 3, written)
		assert.Equal(t, http.StatusPartialContent, recorder.Code)
		assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
		assert.Equal(t, []byte{0x00, 0x01, 0x02}, recorder.Body.Bytes())
	})

	// SequentialWritesConcatenate asserts a copied upstream body arrives in
	// order and unmodified, which is what io.Copy produces against this writer.
	t.Run("SequentialWritesConcatenate", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		for _, chunk := range []string{"chunk-1", "chunk-2", "chunk-3"} {
			written, err := stream.Write([]byte(chunk))
			require.NoError(t, err)
			assert.Equal(t, len(chunk), written)
		}

		assert.Equal(t, "chunk-1chunk-2chunk-3", recorder.Body.String())
	})

	// FlushReachesTheClient asserts proxied bytes are pushed incrementally
	// rather than held until the handler returns.
	t.Run("FlushReachesTheClient", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		_, err = stream.Write([]byte("partial"))
		require.NoError(t, err)
		require.NoError(t, stream.Flush())

		assert.True(t, recorder.Flushed)
		assert.Equal(t, "partial", recorder.Body.String())
	})

	// AddHeaderKeepsRepeatedValues asserts the append accessor does not collapse
	// onto the last value. Codex forwards X-Reasoning-Included and
	// X-Codex-Turn-State as repeats, and an implementation that aliased
	// AddHeader onto SetHeader would silently drop all but one.
	t.Run("AddHeaderKeepsRepeatedValues", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/responses", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		stream.AddHeader("X-Codex-Turn-State", "first")
		stream.AddHeader("X-Codex-Turn-State", "second")

		assert.Equal(t, []string{"first", "second"}, recorder.Header().Values("X-Codex-Turn-State"),
			"AddHeader must append rather than replace")
	})

	// SetHeaderReplacesAndHeaderReadsBack asserts the read accessor observes
	// staged headers, which is what lets the SSE renderer install Cache-Control
	// only when nothing stricter is already set.
	t.Run("SetHeaderReplacesAndHeaderReadsBack", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/responses", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		assert.Empty(t, stream.Header("Cache-Control"), "an unset header must read back empty")

		stream.SetHeader("Cache-Control", "no-store")
		assert.Equal(t, "no-store", stream.Header("Cache-Control"))

		stream.SetHeader("Cache-Control", "no-cache")
		assert.Equal(t, "no-cache", stream.Header("Cache-Control"), "SetHeader must replace")
		assert.Equal(t, []string{"no-cache"}, recorder.Header().Values("Cache-Control"))
	})

	// CanFlushAgreesWithFlushAndHasNoSideEffect asserts the capability probe is
	// consistent with the operation it describes, and that probing does not
	// itself commit the response. A probe implemented by attempting a flush
	// would send the status code early on every stream.
	t.Run("CanFlushAgreesWithFlushAndHasNoSideEffect", func(t *testing.T) {
		adapted, recorder := adapter.NewContext(httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		require.True(t, stream.CanFlush(), "a recorder-backed stream can flush")
		assert.False(t, recorder.Flushed, "probing the capability must not flush")

		require.NoError(t, stream.Flush())
		assert.True(t, recorder.Flushed, "CanFlush must agree with Flush")
	})

	// SetWriteDeadlineReportsSupport asserts the deadline capability answers
	// rather than failing the request. An in-process recorder has no connection
	// underneath, so false is the correct answer there and callers treat the
	// bound as best-effort.
	t.Run("SetWriteDeadlineReportsSupport", func(t *testing.T) {
		adapted, _ := adapter.NewContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		stream, err := adapter.NewResponseStream(adapted)
		require.NoError(t, err)

		assert.False(t, stream.SetWriteDeadline(time.Now().Add(time.Minute)),
			"a stream with no connection underneath must report the deadline as unsupported")
	})
}
