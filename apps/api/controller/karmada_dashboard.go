package controller

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/service"
)

func CreateKarmadaDashboardSession(c contract.Context) {
	identity, ok := security.GetSessionAuthIdentity(c)
	if !ok {
		ops.AbortWithMessage(c, 401, "unauthorized")
		return
	}
	ops.CreateKarmadaDashboardSession(c, identity)
}

func ProxyKarmadaDashboard(c contract.Context) {
	sessionCookie, err := c.Cookie("newapi_karmada_session")
	if err != nil {
		ops.AbortWithMessage(c, 401, "missing karmada dashboard session cookie")
		return
	}

	ident, err := service.ValidateKarmadaDashboardSession(sessionCookie)
	if err != nil {
		ops.AbortWithMessage(c, 401, "invalid karmada dashboard session")
		return
	}

	_, user, err := identity.ValidateLoginSession(ident)
	if err != nil || user.Role != common.RoleRootUser || user.Status != common.UserStatusEnabled {
		ops.AbortWithMessage(c, 403, "forbidden")
		return
	}

	ops.ProxyKarmadaDashboard(c)
}
