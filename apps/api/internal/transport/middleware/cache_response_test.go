package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"

	"github.com/stretchr/testify/assert"
)

// TestCacheDirectiveByPath pins the path-based Cache-Control split so a future
// refactor cannot silently downgrade hashed static assets or upgrade the root.
func TestCacheDirectiveByPath(t *testing.T) {
	const wantCacheVersion = "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14"

	for _, tc := range []struct {
		name             string
		target           string
		wantCacheControl string
	}{
		{name: "root is no-cache", target: "/", wantCacheControl: "no-cache"},
		{name: "hashed js asset is immutable", target: "/static/js/index.abc123.js", wantCacheControl: "public, max-age=31536000, immutable"},
		{name: "hashed css asset is immutable", target: "/static/css/x.css", wantCacheControl: "public, max-age=31536000, immutable"},
		{name: "query string on static stays immutable", target: "/static/js/app.abc123.js?v=1", wantCacheControl: "public, max-age=31536000, immutable"},
		{name: "spa fallback keeps one week", target: "/dashboard", wantCacheControl: "max-age=604800"},
		{name: "nested spa route keeps one week", target: "/settings/models", wantCacheControl: "max-age=604800"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodGet, tc.target, nil))

			Cache()(ctx)

			assert.Equal(t, tc.wantCacheControl, recorder.Header().Get("Cache-Control"))
			assert.Equal(t, wantCacheVersion, recorder.Header().Get("Cache-Version"))
		})
	}
}
