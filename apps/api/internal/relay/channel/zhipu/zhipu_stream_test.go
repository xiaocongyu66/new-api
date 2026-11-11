package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZhipuStreamHandlerDoneSentinel(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "data done sentinel is skipped, not emitted as content",
			body: `data: [DONE]`,
		},
		{
			name: "content chunk then data done sentinel terminates",
			body: `data: hello
data: [DONE]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}

			_, apiErr := zhipuStreamHandler(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "chatglm"},
			}, resp)
			require.Nil(t, apiErr)

			// The handler always terminates the SSE stream with "data: [DONE]".
			// The sentinel must be skipped, not forwarded as a content chunk, so
			// exactly one occurrence proves it never entered the data channel.
			out := w.Body.String()
			assert.Equal(t, 1, strings.Count(out, "[DONE]"), "sentinel forwarded as content:\n%s", out)
		})
	}
}
