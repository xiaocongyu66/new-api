package compose

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebRouterFallbackDispatch pins the SetWebRouter contract.
//
// SetWebRouter registers no named routes, so a method/path snapshot cannot cover
// it. Its actual contract is the NoRoute fallback split: unmatched API-shaped
// prefixes must return the relay not-found JSON so SDK clients get a structured
// error, while any other unmatched path must serve the SPA index so client-side
// routes deep-link correctly. Swapping that branch would either break API error
// handling or serve HTML to API clients.
func TestWebRouterFallbackDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const indexMarker = "<!doctype html><title>spa-index</title>"

	for _, tc := range []struct {
		name          string
		target        string
		wantStatus    int
		wantIndexBody bool
	}{
		{name: "unmatched api path returns relay not found", target: "/api/does-not-exist", wantStatus: http.StatusNotFound},
		{name: "unmatched v1 path returns relay not found", target: "/v1/does-not-exist", wantStatus: http.StatusNotFound},
		{name: "unmatched assets path returns relay not found", target: "/assets/missing.js", wantStatus: http.StatusNotFound},
		{name: "spa route serves index", target: "/channels", wantStatus: http.StatusOK, wantIndexBody: true},
		{name: "nested spa route serves index", target: "/settings/models", wantStatus: http.StatusOK, wantIndexBody: true},
		{name: "root serves index", target: "/", wantStatus: http.StatusOK, wantIndexBody: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			SetWebRouter(ginadapter.WrapEngine(engine), WebAssets{IndexPage: []byte(indexMarker)})

			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.target, nil))

			require.Equal(t, tc.wantStatus, recorder.Code)

			if tc.wantIndexBody {
				assert.Equal(t, indexMarker, recorder.Body.String())
				assert.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
				assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
				return
			}

			assert.NotContains(t, recorder.Body.String(), indexMarker,
				"API-shaped paths must not receive the SPA index")
		})
	}
}
