package compose

import (
	"github.com/QuantumNous/new-api/internal/identity/policy"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/QuantumNous/new-api/internal/security"
	"github.com/gin-gonic/gin"
)

// registerAuthzRoutes mounts the authorization API under its own /authz
// namespace. GET /authz/catalog returns the permission schema (resources,
// actions, and role baselines) used by the client permission editor.
func registerAuthzRoutes(apiRouter *gin.RouterGroup) {
	authzRoute := apiRouter.Group("/authz")
	authzRoute.Use(ginadapter.Middleware(security.AdminAuth()))
	{
		authzRoute.GET("/catalog", ginadapter.Handler(policy.GetPermissionCatalogHandler))
	}
}
