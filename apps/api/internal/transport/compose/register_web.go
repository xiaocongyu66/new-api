package compose

import (
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/internal/transport/static"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// SetWebRouter applies web middleware chain and registers static file serving.
// WebAssets is re-exported from static package.
type WebAssets = static.WebAssets

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(ginadapter.Middleware(middleware.GlobalWebRateLimit()))
	router.Use(ginadapter.Middleware(middleware.Cache()))
	static.ServeStatic(router, assets)
}
