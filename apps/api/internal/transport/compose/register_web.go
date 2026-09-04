package compose

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/internal/transport/static"
)

// SetWebRouter applies web middleware chain and registers static file serving.
// WebAssets is re-exported from static package.
type WebAssets = static.WebAssets

func SetWebRouter(router contract.Engine, assets WebAssets) {
	router.UseCompression()
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	static.ServeStatic(router, assets)
}
