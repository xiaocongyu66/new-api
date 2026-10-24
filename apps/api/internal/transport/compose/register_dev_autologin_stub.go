//go:build !dev

package compose

import "github.com/QuantumNous/new-api/internal/transport/contract"

// registerDevAutoLogin is intentionally empty. A baked-in
// `/api/user/dev/auto-login` is a credential bypass, so it must never exist in
// a production binary: the `dev`-tagged sibling carries the registration, and
// this build compiles without it. The route then fails to match and falls
// through to the SPA fallback instead of handing out a session.
func registerDevAutoLogin(userRoute contract.Routes) {}
