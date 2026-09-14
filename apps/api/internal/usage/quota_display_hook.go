package usage

// OnQuotaToDisplayAmount converts an internal quota integer to the amount in
// the site's configured display currency. The billing domain owns the display
// settings and already imports this package, so it registers the conversion
// from its own init() rather than this domain importing billing (same
// convention as identity.OnQuotaToDisplayAmount).
//
// Unregistered means billing has not been wired (a unit test that never loads
// the billing domain). The raw quota is its own display value in that case, so
// falling back to float64(quota) keeps responses finite instead of panicking.
var OnQuotaToDisplayAmount func(quota int) float64

// quotaToDisplayAmount renders an internal quota value for an API response. It
// is the sole way a `_display` sibling field in this package is produced, so
// the number the frontend renders can never drift from billing's rendering.
func quotaToDisplayAmount(quota int) float64 {
	if OnQuotaToDisplayAmount == nil {
		return float64(quota)
	}
	return OnQuotaToDisplayAmount(quota)
}
