package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBodyRequestContext builds a context carrying a JSON body, matching how the
// relay entrypoints receive a client request.
func newBodyRequestContext(t *testing.T, body string) *gin.Context {
	t.Helper()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// TestUnmarshalBodyReusableAllowsRepeatedDecode pins the multi-read contract the
// relay pipeline depends on: middleware inspects the body to pick a model, then
// the adaptor decodes it again. Losing replay would break every relay request.
func TestUnmarshalBodyReusableAllowsRepeatedDecode(t *testing.T) {
	c := newBodyRequestContext(t, `{"model":"gpt-4","stream":true}`)

	var first struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	require.NoError(t, UnmarshalBodyReusable(c, &first))
	assert.Equal(t, "gpt-4", first.Model)
	assert.True(t, first.Stream)

	var second struct {
		Model string `json:"model"`
	}
	require.NoError(t, UnmarshalBodyReusable(c, &second))
	assert.Equal(t, "gpt-4", second.Model)
}

// TestUnmarshalBodyReusableRestoresRequestBodyForDownstreamReader asserts the
// request body is still readable after decoding, because the outbound relay
// forwards it to the upstream provider.
func TestUnmarshalBodyReusableRestoresRequestBodyForDownstreamReader(t *testing.T) {
	payload := `{"model":"claude-3","messages":[]}`
	c := newBodyRequestContext(t, payload)

	var decoded struct {
		Model string `json:"model"`
	}
	require.NoError(t, UnmarshalBodyReusable(c, &decoded))
	require.Equal(t, "claude-3", decoded.Model)

	forwarded, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(forwarded))
}

// TestGetRequestBodySeeksToStartOnRepeatedAccess pins that the replay storage is
// rewound for each caller instead of returning a drained reader.
func TestGetRequestBodySeeksToStartOnRepeatedAccess(t *testing.T) {
	payload := `{"model":"gemini-pro"}`
	c := newBodyRequestContext(t, payload)

	storage, err := GetBodyStorage(c)
	require.NoError(t, err)

	firstReader, err := storage.NewReader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = firstReader.Close() })
	firstRead, err := io.ReadAll(firstReader)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(firstRead))

	secondReader, err := storage.NewReader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = secondReader.Close() })
	secondRead, err := io.ReadAll(secondReader)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(secondRead), "each reader starts at the payload start")
}

// TestCleanupBodyStorageReleasesCachedBody asserts the per-request cleanup path
// clears the cached storage so pooled contexts do not leak a prior body.
func TestCleanupBodyStorageReleasesCachedBody(t *testing.T) {
	c := newBodyRequestContext(t, `{"model":"gpt-4"}`)

	_, err := GetBodyStorage(c)
	require.NoError(t, err)

	CleanupBodyStorage(c)

	cached, exists := c.Get(KeyBodyStorage)
	assert.True(t, exists, "cleanup keeps the key present")
	assert.Nil(t, cached, "cleanup clears the cached storage value")
}
