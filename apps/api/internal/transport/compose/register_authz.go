package compose

import (
	"github.com/QuantumNous/new-api/internal/identity/policy"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/internal/security"
)

// registerAuthzRoutes mounts the authorization API under its own /authz
// namespace. GET /authz/catalog returns the permission schema (resources,
// actions, and role baselines) used by the client permission editor.
func registerAuthzRoutes(apiRouter contract.Routes) {
	authzRoute := apiRouter.Group("/authz")
	authzRoute.Use(security.AdminAuth())
	{
		authzRoute.GET("/catalog", policy.GetPermissionCatalogHandler)
	}
}
