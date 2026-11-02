package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// StreamScannerHandler 用 StreamingTimeout 建 ticker，零值会 panic。
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

// setupScannerTest builds the minimal context/response/relayInfo triple the
// scanner engine needs. Data collection needs no extra locking: cleanup() runs
// wg.Wait() before StreamScannerHandler returns, so every shared slice is
// finalized by then.
func setupScannerTest(t *testing.T, body string, dataHandler func(data string, sr *StreamResult)) (contract.Context, *relaycommon.RelayInfo) {
	t.Helper()
	c, _ := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	StreamScannerHandler(c, resp, info, dataHandler)
	return c, info
}

// A bare "[DONE]" line must terminate the stream instead of being sliced by the
// data-prefix strip into "]" and forwarded as a data line, which used to leave
// soft parse errors and a missing done end reason.
func TestStreamScannerHandler_TerminatesOnDoneSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantEnd   relaycommon.StreamEndReason
		wantLines []string
	}{
		{
			name:      "data prefixed done",
			body:      "data: {\"id\":1}\ndata: [DONE]\n",
			wantEnd:   relaycommon.StreamEndReasonDone,
			wantLines: []string{"{\"id\":1}"},
		},
		{
			name:      "bare done sentinel",
			body:      "data: {\"id\":1}\n[DONE]\n",
			wantEnd:   relaycommon.StreamEndReasonDone,
			wantLines: []string{"{\"id\":1}"},
		},
		{
			name:      "bare done without data prefix not forwarded as data",
			body:      "[DONE]\n",
			wantEnd:   relaycommon.StreamEndReasonDone,
			wantLines: nil,
		},
		{
			name:      "data colon done without space",
			body:      "data:[DONE]\n",
			wantEnd:   relaycommon.StreamEndReasonDone,
			wantLines: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received []string
			_, info := setupScannerTest(t, tt.body, func(data string, sr *StreamResult) {
				received = append(received, data)
			})

			require.NotNil(t, info.StreamStatus)
			assert.Equal(t, tt.wantEnd, info.StreamStatus.GetEndReason())
			assert.Equal(t, tt.wantLines, received)
		})
	}
}
