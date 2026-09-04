package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetEventStreamHeadersSharesAdapterFlagKey asserts the legacy relay helper
// and the transport adapter agree on the "headers already written" flag.
//
// Both write the same streaming headers, and during the migration a single
// response can pass through both paths. If they used different keys the headers
// would be emitted twice for one response, so the shared key is a contract
// rather than an implementation detail.
func TestSetEventStreamHeadersSharesAdapterFlagKey(t *testing.T) {
	c, _ := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	SetEventStreamHeaders(c)

	flag, exists := c.Get(contract.EventStreamHeadersKey)
	require.True(t, exists, "helper must record the flag under the shared contract key")
	assert.Equal(t, true, flag)
}

// TestAdapterStreamSkipsHeadersAlreadyWrittenByHelper asserts the adapter honours
// a flag set by the legacy helper, so a mixed-path response writes the streaming
// headers exactly once.
func TestAdapterStreamSkipsHeadersAlreadyWrittenByHelper(t *testing.T) {
	c, recorder := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	SetEventStreamHeaders(c)

	c.EventStream().SetHeaders()

	assert.Equal(t, []string{"text/event-stream"}, recorder.Header().Values("Content-Type"))
	assert.Equal(t, []string{"no-cache"}, recorder.Header().Values("Cache-Control"))
	assert.Equal(t, []string{"keep-alive"}, recorder.Header().Values("Connection"))
}
