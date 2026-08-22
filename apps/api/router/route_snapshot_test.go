package router

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisteredRoutesMatchSnapshot pins the registered method+path set of every
// router group. Route registration is rewritten during the transport refactor
// (and again when the HTTP framework is replaced), so an accidental path,
// method, or wildcard change must fail here rather than reach clients.
func TestRegisteredRoutesMatchSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		group    string
		register func(*gin.Engine)
	}{
		{group: "api", register: SetApiRouter},
		{group: "dashboard", register: SetDashboardRouter},
		{group: "relay", register: SetRelayRouter},
		{group: "video", register: SetVideoRouter},
	} {
		t.Run(tc.group, func(t *testing.T) {
			engine := gin.New()
			tc.register(engine)

			registered := make([]string, 0, len(engine.Routes()))
			for _, route := range engine.Routes() {
				registered = append(registered, route.Method+" "+route.Path)
			}
			sort.Strings(registered)

			expected := readRouteSnapshot(t, tc.group)
			assert.Equal(t, expected, registered)
		})
	}
}

// TestRelayRouterRegistersStreamingEndpoints keeps the endpoints whose response
// bodies are written incrementally (SSE, websocket upgrade, raw proxy) present
// and on the expected method, because they use response-writer paths that the
// transport refactor replaces.
func TestRelayRouterRegistersStreamingEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	registered := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"POST /v1/chat/completions",
		"POST /v1/messages",
		"POST /v1/responses",
		"POST /pg/chat/completions",
		"GET /v1/realtime",
		"POST /v1beta/models/*path",
	} {
		_, ok := registered[route]
		assert.True(t, ok, "streaming-capable route missing: %s", route)
	}
}

// TestVideoRouterRegistersTaskContentRoutes protects the video endpoints that
// stream upstream bytes straight to the client writer.
func TestVideoRouterRegistersTaskContentRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	registered := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /v1/videos/:task_id/content",
		"GET /v1/video/generations/:task_id",
		"POST /v1/videos/:video_id/remix",
	} {
		_, ok := registered[route]
		assert.True(t, ok, "video route missing: %s", route)
	}
}

func readRouteSnapshot(t *testing.T, group string) []string {
	t.Helper()

	raw, err := os.ReadFile("testdata/routes_" + group + ".txt")
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	routes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		routes = append(routes, line)
	}
	require.NotEmpty(t, routes)
	return routes
}
