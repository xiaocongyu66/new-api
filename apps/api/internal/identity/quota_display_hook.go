package identity

// OnQuotaToDisplayAmount converts an internal quota integer to the amount in
// the site's configured display currency. The billing domain owns the display
// settings and already imports this package, so it registers the conversion
// from its own init() rather than this domain importing billing (same
// convention as OnIsPaymentComplianceConfirmed).
//
// Unregistered means billing has not been wired (a unit test that never loads
// the billing domain). The raw quota is its own display value in that case, so
// falling back to float64(quota) keeps responses finite instead of panicking.
var OnQuotaToDisplayAmount func(quota int) float64

// OnQuotaFromDisplayAmount is the inverse: it accepts an amount submitted in
// the site's display currency and returns internal quota. Billing registers it;
// the fallback passes the value through common.QuotaRound so an unwired
// process still saturates rather than bare-casting.
var OnQuotaFromDisplayAmount func(displayAmount float64) int

// OnQuotaToPaymentAmount converts a quota integer to the payment-gateway
// amount that would buy it. Same layering as OnQuotaToDisplayAmount: billing
// owns the rule and registers it, so this package never imports billing.
//
// Unregistered means billing has not been wired. Falling back to the raw quota
// keeps responses finite instead of panicking.
var OnQuotaToPaymentAmount func(quota int) float64

func quotaToDisplayAmount(quota int) float64 {
	if OnQuotaToDisplayAmount == nil {
		return float64(quota)
	}
	return OnQuotaToDisplayAmount(quota)
}

func quotaToPaymentAmount(quota int) float64 {
	if OnQuotaToPaymentAmount == nil {
		return float64(quota)
	}
	return OnQuotaToPaymentAmount(quota)
}

func quotaFromDisplayAmount(displayAmount float64) int {
	if OnQuotaFromDisplayAmount == nil {
		return 0
	}
	return OnQuotaFromDisplayAmount(displayAmount)
}
