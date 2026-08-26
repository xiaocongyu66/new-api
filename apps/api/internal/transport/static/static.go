package static

import (
	"embed"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	tpmw "github.com/QuantumNous/new-api/internal/transport/middleware"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

// ServeStatic registers static file serving and SPA fallback for the dashboard.
// The middleware chain (gzip, rate limit, cache) should be applied by the caller.
func ServeStatic(router *gin.Engine, assets WebAssets) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.Use(static.Serve("/", frontendFS))
	router.NoRoute(func(cc *gin.Context) {
		cc.Set(tpmw.RouteTagKey, "web")
		uri := cc.Request.RequestURI
		if strings.HasPrefix(uri, "/v1") || strings.HasPrefix(uri, "/api") || strings.HasPrefix(uri, "/assets") {
			controller.RelayNotFound(ginadapter.Wrap(cc))
			return
		}
		cc.Header("Cache-Control", "no-cache")
		cc.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
	})
}
