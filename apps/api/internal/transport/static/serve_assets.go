package static

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	handler "github.com/QuantumNous/new-api/internal/transport/handler"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

// ServeStatic registers static file serving and SPA fallback for the dashboard.
// The middleware chain (gzip, rate limit, cache) should be applied by the caller.
func ServeStatic(router contract.Engine, assets WebAssets) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.ServeAssets("/", frontendFS)
	router.NoRoute(func(cc contract.Context) {
		cc.Set(middleware.RouteTagKey, "web")
		uri := cc.RequestURI()
		if strings.HasPrefix(uri, "/v1") || strings.HasPrefix(uri, "/api") || strings.HasPrefix(uri, "/assets") {
			handler.RelayNotFound(cc)
			return
		}
		cc.SetHeader("Cache-Control", "no-cache")
		_ = cc.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
	})
}
