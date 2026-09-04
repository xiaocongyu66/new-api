package compose

import (
	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/internal/transport/middleware"
)

func SetDashboardRouter(router contract.Engine) {
	apiRouter := router.Group("/")
	apiRouter.Use(middleware.RouteTag("old_api"))
	apiRouter.UseCompression()
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	apiRouter.Use(security.TokenAuth())
	{
		apiRouter.GET("/dashboard/billing/subscription", billing.GetSubscription)
		apiRouter.GET("/v1/dashboard/billing/subscription", billing.GetSubscription)
		apiRouter.GET("/dashboard/billing/usage", billing.GetUsage)
		apiRouter.GET("/v1/dashboard/billing/usage", billing.GetUsage)
	}
}
