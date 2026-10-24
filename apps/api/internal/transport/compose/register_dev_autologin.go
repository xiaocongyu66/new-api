//go:build dev

package compose

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
)

// registerDevAutoLogin mounts the credential-free dev login. The handler lives
// in identity, but the route has to be assembled here: mounting its
// middleware from identity would import a package that imports identity back.
func registerDevAutoLogin(userRoute contract.Routes) {
	userRoute.POST(
		"/dev/auto-login",
		middleware.CriticalRateLimit(),
		middleware.DisableCache(),
		identity.DevAutoLogin,
	)
}
