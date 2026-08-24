package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/capabilities/integration"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func CreateKarmadaDashboardSession(c contract.Context) {
	identity, ok := security.GetSessionAuthIdentity(c)
	if !ok {
		integration.AbortWithMessage(c, 401, "unauthorized")
		return
	}
	integration.CreateKarmadaDashboardSession(c, identity)
}

func ProxyKarmadaDashboard(c contract.Context) {
	sessionCookie, err := c.Cookie("newapi_karmada_session")
	if err != nil {
		integration.AbortWithMessage(c, 401, "missing karmada dashboard session cookie")
		return
	}

	ident, err := integration.ValidateKarmadaDashboardSession(sessionCookie)
	if err != nil {
		integration.AbortWithMessage(c, 401, "invalid karmada dashboard session")
		return
	}

	_, user, err := identity.ValidateLoginSession(ident)
	if err != nil || user.Role != common.RoleRootUser || user.Status != common.UserStatusEnabled {
		integration.AbortWithMessage(c, 403, "forbidden")
		return
	}

	integration.ProxyKarmadaDashboard(c)
}