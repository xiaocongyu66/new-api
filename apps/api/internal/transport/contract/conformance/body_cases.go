package conformance

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runBodyCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// BindJSONKeepsBodyReplayable is the load-bearing assertion of the adapter:
	// decoding through the contract must leave the body readable, since the
	// relay pipeline decodes once for routing and again to forward upstream.
	t.Run("BindJSONKeepsBodyReplayable", func(t *testing.T) {
		payload := `{"model":"gpt-4","stream":true}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		var decoded struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		require.NoError(t, adapted.BindJSON(&decoded))
		assert.Equal(t, "gpt-4", decoded.Model)
		assert.True(t, decoded.Stream)

		raw, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(raw))

		reader, err := adapted.BodyReader()
		require.NoError(t, err)
		t.Cleanup(func() { _ = reader.Close() })
		forwarded, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, payload, string(forwarded))
	})

	// BodyReaderHandsOutIndependentCursors asserts two readers do not share
	// seek state. Retry logic builds a fresh reader per attempt while an earlier
	// one may still be draining, so a shared cursor would forward a truncated
	// body on retry.
	t.Run("BodyReaderHandsOutIndependentCursors", func(t *testing.T) {
		payload := `{"model":"gpt-4"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		first, err := adapted.BodyReader()
		require.NoError(t, err)
		t.Cleanup(func() { _ = first.Close() })

		head := make([]byte, 4)
		_, err = io.ReadFull(first, head)
		require.NoError(t, err)
		assert.Equal(t, `{"mo`, string(head))

		second, err := adapted.BodyReader()
		require.NoError(t, err)
		t.Cleanup(func() { _ = second.Close() })

		whole, err := io.ReadAll(second)
		require.NoError(t, err)
		assert.Equal(t, payload, string(whole),
			"a second reader must start at the body start, independent of the first")

		rest, err := io.ReadAll(first)
		require.NoError(t, err)
		assert.Equal(t, payload[4:], string(rest),
			"the first reader must keep its own position after a second reader ran")
	})

	// RawBodyIsRepeatable asserts the buffered body survives repeated reads,
	// which is what lets logging, billing, and forwarding each read it once.
	t.Run("RawBodyIsRepeatable", func(t *testing.T) {
		payload := `{"model":"gpt-4","stream":false}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		first, err := adapted.RawBody()
		require.NoError(t, err)
		second, err := adapted.RawBody()
		require.NoError(t, err)

		assert.Equal(t, payload, string(first))
		assert.Equal(t, payload, string(second))
	})

	// ReplaceBodyIsObservedByEveryReader asserts a protocol adapter that
	// rewrites the inbound payload wins over the buffered original. If the old
	// bytes survived anywhere, the rewritten request would be forwarded with the
	// pre-translation body.
	t.Run("ReplaceBodyIsObservedByEveryReader", func(t *testing.T) {
		original := `{"model":"claude-3","max_tokens":16}`
		replacement := `{"model":"gpt-4","max_completion_tokens":16}`
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(original))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		// Read first, so the replacement has to invalidate a populated buffer
		// rather than a cold one.
		raw, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, original, string(raw))

		adapted.ReplaceBody([]byte(replacement))

		replaced, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, replacement, string(replaced), "RawBody must observe the replacement")

		reader, err := adapted.BodyReader()
		require.NoError(t, err)
		t.Cleanup(func() { _ = reader.Close() })
		streamed, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, replacement, string(streamed), "BodyReader must observe the replacement")

		var decoded struct {
			Model string `json:"model"`
		}
		require.NoError(t, adapted.BindJSON(&decoded))
		assert.Equal(t, "gpt-4", decoded.Model, "BindJSON must decode the replacement, not the original")

		assert.Equal(t, int64(len(replacement)), adapted.ContentLength(),
			"ContentLength must track the replacement so the outbound request is framed correctly")
	})

	// ReplaceBodyIsSequential asserts consecutive replacements apply in order
	// and only the last one survives. Several adapters may rewrite one payload
	// in turn (protocol translation then parameter normalisation).
	t.Run("ReplaceBodyIsSequential", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"step":0}`))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		for step := 1; step <= 3; step++ {
			adapted.ReplaceBody([]byte(`{"step":` + string(rune('0'+step)) + `}`))

			raw, err := adapted.RawBody()
			require.NoError(t, err)
			assert.JSONEq(t, `{"step":`+string(rune('0'+step))+`}`, string(raw),
				"each replacement must be visible before the next one")
		}

		final, err := adapted.RawBody()
		require.NoError(t, err)
		assert.JSONEq(t, `{"step":3}`, string(final), "only the last replacement survives")
	})

	// ReplaceBodyThenResetBodyKeepsTheReplacement asserts the rewind path used
	// between retry attempts rewinds to the current payload rather than
	// resurrecting the pre-replacement bytes.
	t.Run("ReplaceBodyThenResetBodyKeepsTheReplacement", func(t *testing.T) {
		replacement := `{"model":"gpt-4","attempt":1}`
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		adapted.ReplaceBody([]byte(replacement))

		// Drain through the escape hatch the way a forwarding attempt does, so
		// the rewind has to restore an exhausted body.
		drained, err := io.ReadAll(adapted.HTTPRequest().Body)
		require.NoError(t, err)
		assert.JSONEq(t, replacement, string(drained))

		adapted.ResetBody(io.NopCloser(bytes.NewReader([]byte(replacement))))

		replayed, err := io.ReadAll(adapted.HTTPRequest().Body)
		require.NoError(t, err)
		assert.JSONEq(t, replacement, string(replayed),
			"after ResetBody the downstream reader must see the full payload again")
	})

	// ResetBodyIsRepeatable asserts alternating resets and reads stay in order,
	// which is the retry loop's actual access pattern.
	t.Run("ResetBodyIsRepeatable", func(t *testing.T) {
		payload := []byte(`{"model":"gpt-4"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		adapted, _, _ := adapter.NewContext(req)

		for attempt := 0; attempt < 3; attempt++ {
			adapted.ResetBody(io.NopCloser(bytes.NewReader(payload)))

			forwarded, err := io.ReadAll(adapted.HTTPRequest().Body)
			require.NoError(t, err)
			assert.Equal(t, string(payload), string(forwarded),
				"attempt %d must observe the whole body", attempt)
		}
	})
}
