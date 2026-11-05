package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
)

// countHandler writes the given status and body, counting invocations.
func countHandler(calls *int, status int, body string) contract.Handler {
	return func(c contract.Context) {
		*calls++
		_ = c.Data(status, "application/json; charset=utf-8", []byte(body))
	}
}

// A second GET must be served from the cache without running the handler, and
// the cached body must be the response byte-for-byte. A hit that writes without
// aborting would let the chain loop append the handler's body a second time,
// which is exactly what this pins.
func TestReadOnlyCacheServesSecondGetWithoutRunningHandler(t *testing.T) {
	calls := 0
	handler := countHandler(&calls, http.StatusOK, `{"ok":true}`)
	cache := ReadOnlyCache(ReadOnlyCacheConfig{Namespace: uniqueNamespace("test_ro_hit"), TTL: CacheTTLLong})

	first := testutil.ServeBufferedRoute(t, http.MethodGet, "/cached", []contract.Middleware{cache}, handler,
		httptest.NewRequest(http.MethodGet, "/cached", nil))
	body1 := readBody(t, first)

	second := testutil.ServeBufferedRoute(t, http.MethodGet, "/cached", []contract.Middleware{cache}, handler,
		httptest.NewRequest(http.MethodGet, "/cached", nil))
	body2 := readBody(t, second)

	require.Equal(t, 1, calls, "the handler must run only on the miss")
	assert.Equal(t, body1, body2, "a hit must replay exactly the stored body")
	assert.Equal(t, http.StatusOK, second.StatusCode)
}

// A non-GET request is never served from or stored into the cache.
func TestReadOnlyCachePassesThroughNonGet(t *testing.T) {
	calls := 0
	handler := countHandler(&calls, http.StatusOK, `{"ok":true}`)
	cache := ReadOnlyCache(ReadOnlyCacheConfig{Namespace: uniqueNamespace("test_ro_post"), TTL: CacheTTLLong})

	for range 2 {
		testutil.ServeBufferedRoute(t, http.MethodPost, "/write", []contract.Middleware{cache}, handler,
			httptest.NewRequest(http.MethodPost, "/write", nil))
	}

	assert.Equal(t, 2, calls, "a write must reach the handler every time")
}

// A non-200 response is not cacheable: the next request must reach the handler.
func TestReadOnlyCacheSkipsNonOK(t *testing.T) {
	calls := 0
	handler := countHandler(&calls, http.StatusBadRequest, `{"error":"nope"}`)
	cache := ReadOnlyCache(ReadOnlyCacheConfig{Namespace: uniqueNamespace("test_ro_error"), TTL: CacheTTLLong})

	for range 2 {
		resp := testutil.ServeBufferedRoute(t, http.MethodGet, "/err", []contract.Middleware{cache}, handler,
			httptest.NewRequest(http.MethodGet, "/err", nil))
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	}

	assert.Equal(t, 2, calls, "an error response must not be cached")
}

// Two spellings of one logical request must share an entry: the key drops a
// trailing slash and sorts the query, so the handler runs once for both.
func TestReadOnlyCacheKeyIsCanonical(t *testing.T) {
	calls := 0
	handler := countHandler(&calls, http.StatusOK, `{"ok":true}`)
	cache := ReadOnlyCache(ReadOnlyCacheConfig{Namespace: uniqueNamespace("test_ro_key"), TTL: CacheTTLLong})

	testutil.ServeBufferedRoute(t, http.MethodGet, "/canon/", []contract.Middleware{cache}, handler,
		httptest.NewRequest(http.MethodGet, "/canon/?b=2&a=1", nil))
	testutil.ServeBufferedRoute(t, http.MethodGet, "/canon", []contract.Middleware{cache}, handler,
		httptest.NewRequest(http.MethodGet, "/canon?a=1&b=2", nil))

	assert.Equal(t, 1, calls, "a trailing slash and query order must not split the entry")
}

// uniqueNamespace keeps a cached entry from leaking between runs of the same
// test, which would make -count=N see a hit on its first request.
func uniqueNamespace(base string) string {
	return base + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}
