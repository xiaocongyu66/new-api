package compose

import (
	"github.com/QuantumNous/new-api/internal/capabilities/billing"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	tpmw "github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetDashboardRouter(router *gin.Engine) {
	apiRouter := router.Group("/")
	apiRouter.Use(tpmw.RouteTag("old_api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(ginadapter.Middleware(middleware.GlobalAPIRateLimit()))
	apiRouter.Use(ginadapter.Middleware(security.TokenAuth()))
	{
		apiRouter.GET("/dashboard/billing/subscription", ginadapter.Handler(billing.GetSubscription))
		apiRouter.GET("/v1/dashboard/billing/subscription", ginadapter.Handler(billing.GetSubscription))
		apiRouter.GET("/dashboard/billing/usage", ginadapter.Handler(billing.GetUsage))
		apiRouter.GET("/v1/dashboard/billing/usage", ginadapter.Handler(billing.GetUsage))
	}
}
