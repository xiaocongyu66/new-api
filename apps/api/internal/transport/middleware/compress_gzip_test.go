package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecompressRequestMiddlewareReplacesBodyAndStripsEncoding pins what the
// middleware guarantees downstream handlers: the body they read is decompressed,
// and Content-Encoding is gone so nothing tries to decode it a second time.
//
// The middleware reaches through the contract's standard-library escape hatches
// rather than the buffered body accessors, because the size cap has to wrap the
// stream before any reader touches it. That makes it worth asserting directly.
func TestDecompressRequestMiddlewareReplacesBodyAndStripsEncoding(t *testing.T) {
	const payload = `{"model":"gpt-4o","stream":true}`

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	var observedBody string
	var observedEncoding string

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	response := testutil.ServeBufferedRoute(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		[]contract.Middleware{DecompressRequestMiddleware()},
		func(c contract.Context) {
			body, readErr := io.ReadAll(c.HTTPRequest().Body)
			require.NoError(t, readErr)
			observedBody = string(body)
			observedEncoding = c.Header("Content-Encoding")
			c.Status(http.StatusOK)
		},
		request,
	)

	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, payload, observedBody, "the handler must observe the decompressed payload")
	assert.Empty(t, observedEncoding, "Content-Encoding must be stripped once the body is decompressed")
}

// TestDecompressRequestMiddlewareCapsDecompressedSize asserts the cap applies to
// the post-decompression stream, which is the whole reason the middleware wraps
// the body before anything reads it: a small compressed payload can expand past
// the limit, and the read has to fail rather than silently truncate.
func TestDecompressRequestMiddlewareCapsDecompressedSize(t *testing.T) {
	original := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = original })

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := io.Copy(writer, strings.NewReader(strings.Repeat("a", 2<<20)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Less(t, compressed.Len(), 1<<20, "the compressed payload must fit under the cap so only expansion trips it")

	var readErr error

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	testutil.ServeBufferedRoute(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		[]contract.Middleware{DecompressRequestMiddleware()},
		func(c contract.Context) {
			_, readErr = io.ReadAll(c.HTTPRequest().Body)
			c.Status(http.StatusOK)
		},
		request,
	)

	require.Error(t, readErr, "reading past the cap must fail instead of returning a truncated body")
}

// TestDecompressRequestMiddlewareLimitBoundaryPerEncoding pins both halves of the
// cap's contract for every compression format, because each builds a different
// two-layer closer chain around the limited stream: a body sitting exactly at the
// limit must arrive byte-for-byte, and a body one byte over must fail the read.
//
// The oversized half is the security-relevant one. A silently truncated body
// still parses as a legitimate request, so the read has to error rather than
// return short.
func TestDecompressRequestMiddlewareLimitBoundaryPerEncoding(t *testing.T) {
	const limitBytes = 1 << 20

	original := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = original })

	encodings := []struct {
		name     string
		compress func(*testing.T, []byte) []byte
	}{
		{"gzip", func(t *testing.T, payload []byte) []byte {
			var buffer bytes.Buffer
			writer := gzip.NewWriter(&buffer)
			_, err := writer.Write(payload)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			return buffer.Bytes()
		}},
		{"br", func(t *testing.T, payload []byte) []byte {
			var buffer bytes.Buffer
			writer := brotli.NewWriter(&buffer)
			_, err := writer.Write(payload)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			return buffer.Bytes()
		}},
		{"zstd", func(t *testing.T, payload []byte) []byte {
			var buffer bytes.Buffer
			writer, err := zstd.NewWriter(&buffer)
			require.NoError(t, err)
			_, err = writer.Write(payload)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			return buffer.Bytes()
		}},
	}

	sizes := map[string]int{
		"exactly at the limit": limitBytes,
		"one byte over":        limitBytes + 1,
	}

	for _, encoding := range encodings {
		for sizeName, size := range sizes {
			t.Run(encoding.name+"/"+sizeName, func(t *testing.T) {
				payload := bytes.Repeat([]byte("a"), size)
				body := encoding.compress(t, payload)

				var observed []byte
				var readErr error

				request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				request.Header.Set("Content-Encoding", encoding.name)
				testutil.ServeBufferedRoute(
					t,
					http.MethodPost,
					"/v1/chat/completions",
					[]contract.Middleware{DecompressRequestMiddleware()},
					func(c contract.Context) {
						observed, readErr = io.ReadAll(c.HTTPRequest().Body)
						c.Status(http.StatusOK)
					},
					request,
				)

				if size > limitBytes {
					require.Error(t, readErr, "a body past the cap must fail the read instead of arriving truncated")
					require.True(t, common.IsRequestBodyTooLargeError(readErr),
						"the cap must report an oversized body so handlers map it to 413, got %v", readErr)
					assert.NotEqual(t, payload, observed, "an oversized body must not be delivered as if it were complete")
					return
				}

				require.NoError(t, readErr, "a body exactly at the cap must read cleanly")
				require.Len(t, observed, size)
				assert.True(t, bytes.Equal(payload, observed), "a body at the cap must arrive byte-for-byte")
			})
		}
	}
}
