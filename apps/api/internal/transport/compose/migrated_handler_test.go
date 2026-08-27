package compose

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigratedHandlerServesThroughGinAdapter asserts a handler written against the
// transport contract still serves a real request when registered on a gin route.
//
// Handlers are migrated off *gin.Context one package at a time, so the adapter
// seam has to work through actual route registration rather than only in unit
// tests: a handler that compiles but produces no response would otherwise ship.
func TestMigratedHandlerServesThroughGinAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.GET("/api/authz/catalog", ginadapter.Handler(controller.GetPermissionCatalog))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/authz/catalog", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Resources any `json:"resources"`
			Roles     any `json:"roles"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))

	assert.True(t, body.Success)
	assert.NotNil(t, body.Data.Resources, "permission catalog must expose resources")
	assert.NotNil(t, body.Data.Roles, "permission catalog must expose roles")
}

// TestMigratedHandlerReadsQueryThroughContract asserts the contract's query
// accessor, including its default, is what a migrated handler observes.
//
// GetRankings reads `period` via DefaultQuery before reaching its service, and
// that service requires a database, so the query capability is asserted directly
// on the adapted context instead of driving the full handler here. The service
// path keeps its own coverage in the service package.
func TestMigratedHandlerReadsQueryThroughContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name     string
		target   string
		expected string
	}{
		{name: "explicit period", target: "/api/rankings?period=month", expected: "month"},
		{name: "default period", target: "/api/rankings", expected: "week"},
		{name: "blank period falls back", target: "/api/rankings?period=", expected: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var observed string

			engine := gin.New()
			engine.GET("/api/rankings", ginadapter.Handler(func(c contract.Context) {
				observed = c.DefaultQuery("period", "week")
				_ = c.JSON(http.StatusOK, map[string]any{"success": true})
			}))

			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.target, nil))

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tc.expected, observed)
		})
	}
}
