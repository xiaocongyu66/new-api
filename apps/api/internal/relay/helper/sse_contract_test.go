package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/sensitive"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSSERecorder builds a synthetic Fiber context whose writes land in a
// recorder so the exact bytes reaching the client can be asserted.
func newSSERecorder(t *testing.T) (contract.Context, *httptest.ResponseRecorder) {
	t.Helper()

	return fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
}

// disableCompletionSensitiveCheck removes the output filter from the write path
// so these tests assert framing only. Filter behaviour has its own coverage.
func disableCompletionSensitiveCheck(t *testing.T) {
	t.Helper()

	previous := sensitive.CheckSensitiveEnabled
	sensitive.CheckSensitiveEnabled = false
	t.Cleanup(func() {
		sensitive.CheckSensitiveEnabled = previous
	})
}

// TestStringDataWritesSingleSSEFrame pins the wire framing of a normal stream
// chunk: a `data: ` prefix and a blank-line terminator. Clients parse on those
// exact bytes, and the transport refactor replaces the writer underneath.
func TestStringDataWritesSingleSSEFrame(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	c, recorder := newSSERecorder(t)

	require.NoError(t, StringData(c, `{"choices":[{"delta":{"content":"hi"}}]}`))

	assert.Equal(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n", recorder.Body.String())
	assert.True(t, recorder.Flushed)
}

// TestDoneWritesTerminalSentinel pins the terminal sentinel that ends an
// OpenAI-compatible stream.
func TestDoneWritesTerminalSentinel(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	c, recorder := newSSERecorder(t)

	Done(c)

	assert.Equal(t, "data: [DONE]\n\n", recorder.Body.String())
	assert.True(t, recorder.Flushed)
}

// TestObjectDataMarshalsThroughCommonJSON asserts a struct payload reaches the
// client as one framed JSON event.
func TestObjectDataMarshalsThroughCommonJSON(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	c, recorder := newSSERecorder(t)

	require.NoError(t, ObjectData(c, dto.ChatCompletionsStreamResponse{
		Id:     "chatcmpl-1",
		Object: "chat.completion.chunk",
		Model:  "gpt-4",
	}))

	body := recorder.Body.String()
	assert.True(t, len(body) > len("data: "))
	assert.Equal(t, "data: ", body[:6])
	assert.Equal(t, "\n\n", body[len(body)-2:])
	assert.Contains(t, body, `"id":"chatcmpl-1"`)
	assert.Contains(t, body, `"object":"chat.completion.chunk"`)
}

// TestPingDataWritesCommentFrame pins the keep-alive frame. It is an SSE comment
// rather than a data event, so clients must not surface it as content.
func TestPingDataWritesCommentFrame(t *testing.T) {
	c, recorder := newSSERecorder(t)

	require.NoError(t, PingData(c))

	assert.Equal(t, ": PING\n\n", recorder.Body.String())
	assert.True(t, recorder.Flushed)
}

// TestClaudeChunkDataWritesEventAndDataLines pins the Claude streaming framing,
// which carries a named `event:` line before its `data:` line.
//
// The chunk writer emits a trailing newline beyond the blank-line terminator
// because the caller appends "\n" to the data line and the SSE encoder then
// appends "\n\n". Clients tolerate the extra blank line, but it is part of the
// current byte stream, so it is asserted verbatim to catch an unintended change
// when the writer is replaced.
func TestClaudeChunkDataWritesEventAndDataLines(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	c, recorder := newSSERecorder(t)

	ClaudeChunkData(c, dto.ClaudeResponse{Type: "content_block_delta"}, `{"type":"content_block_delta"}`)

	assert.Equal(t, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n\n", recorder.Body.String())
}

// TestClaudeDataWritesEventAndMarshalledPayload pins the non-chunk Claude writer,
// which marshals the response itself and emits exactly one blank-line terminator.
func TestClaudeDataWritesEventAndMarshalledPayload(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	c, recorder := newSSERecorder(t)

	require.NoError(t, ClaudeData(c, dto.ClaudeResponse{Type: "message_start"}))

	assert.Equal(t, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n", recorder.Body.String())
}

// TestSetEventStreamHeadersSetsStreamingHeaders pins the response headers that
// keep proxies and browsers from buffering a stream.
func TestSetEventStreamHeadersSetsStreamingHeaders(t *testing.T) {
	c, recorder := newSSERecorder(t)

	SetEventStreamHeaders(c)
	SetEventStreamHeaders(c)

	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
	assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))
	assert.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
	_, alreadySet := c.Get(contract.EventStreamHeadersKey)
	assert.True(t, alreadySet)
}

// TestStringDataRejectsCancelledRequest asserts a cancelled client aborts the
// write instead of emitting a partial frame.
func TestStringDataRejectsCancelledRequest(t *testing.T) {
	disableCompletionSensitiveCheck(t)
	ctx, cancel := context.WithCancel(context.Background())
	c, recorder := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx))
	cancel()

	err := StringData(c, `{"choices":[]}`)

	require.Error(t, err)
	assert.Empty(t, recorder.Body.String())
}
