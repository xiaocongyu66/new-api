package cloudflare

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

func TestNormalizeStreamLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "data with space", line: `data: {"x":1}`, want: `{"x":1}`},
		{name: "data no space done", line: `data:[DONE]`, want: `[DONE]`},
		{name: "data space done", line: `data: [DONE]`, want: `[DONE]`},
		{name: "data no space json", line: `data:{"x":1,"y":2}`, want: `{"x":1,"y":2}`},
		{name: "leading and trailing whitespace", line: `  data: [DONE]  `, want: `[DONE]`},
		{name: "carriage return", line: "data: [DONE]\r", want: "[DONE]"},
		{name: "bare done sentinel", line: `[DONE]`, want: `[DONE]`},
		{name: "bare payload", line: `hello`, want: `hello`},
		{name: "empty line", line: ``, want: ``},
		{name: "data prefix only", line: `data:`, want: ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeStreamLine(tt.line))
		})
	}
}

func TestCfStreamHandlerDoneSentinel(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "data space done", body: `data: [DONE]`},
		{name: "data no space done (issue #634)", body: `data:[DONE]`},
		{
			name: "chunk then no space done",
			body: `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}
data:[DONE]`,
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

			apiErr, usage := cfStreamHandler(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4"},
			}, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)

			out := w.Body.String()
			assert.Contains(t, out, `data: [DONE]`)
			if strings.Contains(tt.body, `"content":"hi"`) {
				assert.Contains(t, out, `"content":"hi"`)
			}
		})
	}
}
