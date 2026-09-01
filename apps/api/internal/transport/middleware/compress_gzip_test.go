package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
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
	gin.SetMode(gin.TestMode)

	const payload = `{"model":"gpt-4o","stream":true}`

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	var observedBody string
	var observedEncoding string

	ginEngine := gin.New()
	engine := ginadapter.WrapEngine(ginEngine)
	engine.POST("/v1/chat/completions", DecompressRequestMiddleware(), func(c contract.Context) {
		body, readErr := io.ReadAll(c.HTTPRequest().Body)
		require.NoError(t, readErr)
		observedBody = string(body)
		observedEncoding = c.Header("Content-Encoding")
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	ginEngine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, payload, observedBody, "the handler must observe the decompressed payload")
	assert.Empty(t, observedEncoding, "Content-Encoding must be stripped once the body is decompressed")
}

// TestDecompressRequestMiddlewareCapsDecompressedSize asserts the cap applies to
// the post-decompression stream, which is the whole reason the middleware wraps
// the body before anything reads it: a small compressed payload can expand past
// the limit, and the read has to fail rather than silently truncate.
func TestDecompressRequestMiddlewareCapsDecompressedSize(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

	ginEngine := gin.New()
	engine := ginadapter.WrapEngine(ginEngine)
	engine.POST("/v1/chat/completions", DecompressRequestMiddleware(), func(c contract.Context) {
		_, readErr = io.ReadAll(c.HTTPRequest().Body)
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	ginEngine.ServeHTTP(httptest.NewRecorder(), request)

	require.Error(t, readErr, "reading past the cap must fail instead of returning a truncated body")
}
