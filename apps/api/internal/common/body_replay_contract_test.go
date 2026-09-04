package common_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBodyRequestContext builds a context carrying a JSON body, matching how the
// relay entrypoints receive a client request.
func newBodyRequestContext(t *testing.T, body string) (contract.Context, *httptest.ResponseRecorder) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return fiberadapter.NewSyntheticContext(request)
}

// TestUnmarshalBodyReusableAllowsRepeatedDecode pins the multi-read contract the
// relay pipeline depends on: middleware inspects the body to pick a model, then
// the adaptor decodes it again. Losing replay would break every relay request.
func TestUnmarshalBodyReusableAllowsRepeatedDecode(t *testing.T) {
	c, _ := newBodyRequestContext(t, `{"model":"gpt-4","stream":true}`)

	var first struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	require.NoError(t, common.UnmarshalCtxBodyReusable(c, &first))
	assert.Equal(t, "gpt-4", first.Model)
	assert.True(t, first.Stream)

	var second struct {
		Model string `json:"model"`
	}
	require.NoError(t, common.UnmarshalCtxBodyReusable(c, &second))
	assert.Equal(t, "gpt-4", second.Model)
}

// TestUnmarshalBodyReusableRestoresRequestBodyForDownstreamReader asserts the
// request body is still readable after decoding, because the outbound relay
// forwards it to the upstream provider.
func TestUnmarshalBodyReusableRestoresRequestBodyForDownstreamReader(t *testing.T) {
	payload := `{"model":"claude-3","messages":[]}`
	c, _ := newBodyRequestContext(t, payload)

	var decoded struct {
		Model string `json:"model"`
	}
	require.NoError(t, common.UnmarshalCtxBodyReusable(c, &decoded))
	require.Equal(t, "claude-3", decoded.Model)

	reader, err := c.BodyReader()
	require.NoError(t, err)
	forwarded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, payload, string(forwarded))
}

// TestGetRequestBodySeeksToStartOnRepeatedAccess pins that the replay storage is
// rewound for each caller instead of returning a drained reader.
func TestGetRequestBodySeeksToStartOnRepeatedAccess(t *testing.T) {
	payload := `{"model":"gemini-pro"}`
	c, _ := newBodyRequestContext(t, payload)

	storage, err := common.GetBodyStorage(c)
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

// TestReplayableBodyAccessorsAgreeAfterReplaceAndReset pins the rest of the
// replayable-body surface the relay drives: RawBody materialises the payload,
// ReplaceBody retargets it for every later read, BodyStream exposes the live
// reader, and ResetBody reinstalls one.
func TestReplayableBodyAccessorsAgreeAfterReplaceAndReset(t *testing.T) {
	original := `{"model":"gpt-4"}`
	c, _ := newBodyRequestContext(t, original)

	raw, err := c.RawBody()
	require.NoError(t, err)
	assert.JSONEq(t, original, string(raw))

	rewritten := `{"model":"gpt-4o","stream":true}`
	c.ReplaceBody([]byte(rewritten))

	raw, err = c.RawBody()
	require.NoError(t, err)
	assert.JSONEq(t, rewritten, string(raw), "ReplaceBody drops the cached storage")

	var decoded struct {
		Model string `json:"model"`
	}
	require.NoError(t, common.UnmarshalCtxBodyReusable(c, &decoded))
	assert.Equal(t, "gpt-4o", decoded.Model)

	streamed, err := io.ReadAll(c.BodyStream())
	require.NoError(t, err)
	assert.JSONEq(t, rewritten, string(streamed), "decoding reinstalls a readable stream")

	c.ResetBody(io.NopCloser(strings.NewReader(rewritten)))
	streamed, err = io.ReadAll(c.BodyStream())
	require.NoError(t, err)
	assert.JSONEq(t, rewritten, string(streamed))
}

// TestCleanupBodyStorageReleasesCachedBody asserts the per-request cleanup path
// clears the cached storage so pooled contexts do not leak a prior body.
func TestCleanupBodyStorageReleasesCachedBody(t *testing.T) {
	c, _ := newBodyRequestContext(t, `{"model":"gpt-4"}`)

	_, err := common.GetBodyStorage(c)
	require.NoError(t, err)

	common.CleanupBodyStorage(c)

	cached, exists := c.Get(common.KeyBodyStorage)
	assert.True(t, exists, "cleanup keeps the key present")
	assert.Nil(t, cached, "cleanup clears the cached storage value")
}
