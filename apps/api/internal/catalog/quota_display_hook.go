package channel

// OnQuotaToDisplayAmount converts an internal quota integer to the amount in
// the site's configured display currency. The billing domain owns the display
// settings and already imports this package, so it registers the conversion
// from its own init() rather than this domain importing billing (same
// convention as OnResolveTieredBilling).
//
// Unregistered means billing has not been wired (a unit test that never loads
// the billing domain). The raw quota is its own display value in that case, so
// falling back to float64(quota) keeps responses finite instead of panicking.
var OnQuotaToDisplayAmount func(quota int) float64

// resolveChannelUsedQuotaDisplay renders the channel's used quota for the API
// response. It is the sole producer of Channel.UsedQuotaDisplay, so the number
// the frontend renders can never drift from billing's rendering.
func resolveChannelUsedQuotaDisplay(usedQuota int64) float64 {
	if OnQuotaToDisplayAmount == nil {
		return float64(usedQuota)
	}
	return OnQuotaToDisplayAmount(int(usedQuota))
}
