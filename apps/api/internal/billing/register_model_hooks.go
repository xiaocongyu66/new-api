package billing

import "github.com/QuantumNous/new-api/internal/dbinfra"

// This domain owns user subscriptions; dbinfra cannot import it (this package
// imports dbinfra), so the settlement entry point is registered as a hook.
func init() {
	dbinfra.PostConsumeUserSubscriptionDeltaFn = PostConsumeUserSubscriptionDelta
}
