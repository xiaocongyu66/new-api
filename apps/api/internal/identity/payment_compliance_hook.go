package identity

// OnIsPaymentComplianceConfirmed reports whether the operator has accepted the
// current payment compliance terms. The billing domain owns that setting and
// already imports this package, so it registers the check from its own init()
// rather than this domain importing billing (same convention as the
// TransferAffQuota gate above).
//
// Unregistered deliberately means "not confirmed": this is a compliance gate,
// so the absent-hook path must fail closed rather than grant payment flows.
var OnIsPaymentComplianceConfirmed func() bool

func isPaymentComplianceConfirmed() bool {
	return OnIsPaymentComplianceConfirmed != nil && OnIsPaymentComplianceConfirmed()
}
