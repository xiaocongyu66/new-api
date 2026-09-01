package compose

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetiredFrontendAPIRoutes(t *testing.T) {
	engine := newRouteSnapshotEngine()
	SetApiRouter(engine)

	routes := routeSet(*engine.routes)
	assert.Contains(t, routes, http.MethodPost+" /api/system-task/log-cleanup")
	assert.NotContains(t, routes, http.MethodDelete+" /api/log/")
	assert.NotContains(t, routes, http.MethodPost+" /api/option/migrate_console_setting")
}
