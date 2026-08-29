package billing

import "github.com/QuantumNous/new-api/model"

// This domain owns user subscriptions; model cannot import it (this package
// imports model), so the settlement entry point is registered as a hook.
func init() {
	model.PostConsumeUserSubscriptionDeltaFn = PostConsumeUserSubscriptionDelta
}
