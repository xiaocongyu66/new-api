package router

import (
	"embed"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(ginadapter.Middleware(middleware.GlobalWebRateLimit()))
	router.Use(ginadapter.Middleware(middleware.Cache()))
	router.Use(static.Serve("/", frontendFS))
	router.NoRoute(func(cc *gin.Context) {
		cc.Set(middleware.RouteTagKey, "web")
		uri := cc.Request.RequestURI
		if strings.HasPrefix(uri, "/v1") || strings.HasPrefix(uri, "/api") || strings.HasPrefix(uri, "/assets") {
			controller.RelayNotFound(ginadapter.Wrap(cc))
			return
		}
		cc.Header("Cache-Control", "no-cache")
		cc.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
	})
}
