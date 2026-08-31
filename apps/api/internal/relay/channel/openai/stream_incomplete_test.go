package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	relayconstant "github.com/QuantumNous/new-api/internal/relay/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// newStreamTestContext builds the minimal gin/RelayInfo pair OaiStreamHandler
// needs: a chat-completions stream relay against a recorder.
func newStreamTestContext(t *testing.T, upstream string) (contract.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	// StreamScannerHandler builds a ticker from this global; zero panics.
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}

	// The recorder must be the one backing the context, otherwise every body
	// assertion below reads an unwritten buffer.
	c, recorder := ginadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(upstream)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	// Build RelayInfo through the production constructor so unexported state
	// (isFirstResponse) and the StartTime/FirstResponseTime contract match a real
	// request; HasSendResponse() then flips exactly when the first chunk is sent.
	info := relaycommon.GenRelayInfoOpenAI(c, &dto.GeneralOpenAIRequest{Model: "gpt-test"})
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}
	info.IsStream = true
	info.RelayMode = relayconstant.RelayModeChatCompletions
	return c, recorder, resp, info
}

// TestOaiStreamHandlerIncompleteUpstreamStream is the #394 regression: the
// upstream connection dies mid-stream, so no chunk ever carries finish_reason
// and no [DONE] arrives. new-api must not fabricate a terminal [DONE], because
// the client would then treat a truncated answer as a complete one. It must
// surface a retryable error instead.
func TestOaiStreamHandlerIncompleteUpstreamStream(t *testing.T) {

	// Two content chunks, then the upstream body just ends: no finish_reason,
	// no data: [DONE].
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"llo"}}]}

`

	c, recorder, resp, info := newStreamTestContext(t, upstream)

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "incomplete upstream stream must produce an error")
	require.Nil(t, usage, "incomplete stream must not report usage as a success")
	require.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr),
		"partial chunks already reached the client, so retrying would append a second answer")

	body := recorder.Body.String()
	require.NotContains(t, body, "data: [DONE]",
		"new-api must not fabricate a terminal [DONE] for a truncated upstream stream")
	// The failure is reported in-band as a valid SSE event, so a client parsing the
	// stream can tell truncation from a clean finish instead of hitting bare JSON.
	require.Contains(t, body, `data: {"error":`)
	require.Contains(t, body, "upstream stream closed before a finish_reason was received")
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		require.True(t, strings.HasPrefix(line, "data: "),
			"every emitted line must stay valid SSE, got %q", line)
	}
}

// TestOaiStreamHandlerTruncatedBeforeFirstChunkIsRetryable covers the other half
// of the retry decision: the upstream died before any chunk was forwarded, so
// nothing is on the wire yet and the request can safely move to another channel.
func TestOaiStreamHandlerTruncatedBeforeFirstChunkIsRetryable(t *testing.T) {

	c, _, resp, info := newStreamTestContext(t, "")

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.NotNil(t, apiErr)
	require.Nil(t, usage)
	require.False(t, types.IsSkipRetryError(apiErr),
		"no bytes reached the client, so the request is still safe to retry")
}

// TestOaiStreamHandlerDoneWithoutFinishReasonIsComplete pins the second accepted
// terminator: some providers close with `data: [DONE]` and never set
// finish_reason. That is an explicit upstream termination, not a truncation.
func TestOaiStreamHandlerDoneWithoutFinishReasonIsComplete(t *testing.T) {

	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}

data: [DONE]

`

	c, recorder, resp, info := newStreamTestContext(t, upstream)

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

// TestOaiStreamHandlerCompleteUpstreamStream pins the happy path so the #394
// guard cannot reject well-formed streams: a finish_reason chunk followed by
// [DONE] stays a success and still terminates the client stream with [DONE].
func TestOaiStreamHandlerCompleteUpstreamStream(t *testing.T) {

	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	c, recorder, resp, info := newStreamTestContext(t, upstream)

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	body := recorder.Body.String()
	require.Contains(t, body, `"finish_reason":"stop"`)
	require.Contains(t, body, "data: [DONE]")
}
