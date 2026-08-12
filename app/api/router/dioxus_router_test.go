package router

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

//go:embed web/dist/dioxus/index.html
var dioxusTestFS embed.FS

func TestSetDioxusRouterRegistersAssetRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetDioxusRouter(engine, DioxusAssets{BuildFS: dioxusTestFS})

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, hasAssets := routes[http.MethodGet+" /dioxus/*filepath"]
	require.True(t, hasAssets)

	// 面板通过 React 路由 /karmada 的 beforeLoad 守卫保护（仅 super-admin），
	// /dioxus/ 本身不加服务端 auth middleware（与 React SPA 静态资源一致）。
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/dioxus/", nil)
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "main")
}