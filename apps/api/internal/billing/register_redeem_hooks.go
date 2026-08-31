package billing

import "github.com/QuantumNous/new-api/internal/identity"

// This domain owns the Redemption record; identity cannot import it back (this
// package imports identity), so identity.TopUp reaches Redeem through a hook.
//
// Without this registration the hook stays nil and POST /api/user/topup panics
// on a nil func call for every redemption attempt.
func init() {
	identity.RegisterRedemptionRedeemer(Redeem)
}
