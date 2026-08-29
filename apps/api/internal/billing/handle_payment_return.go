package billing

import ()

// paymentReturnPath computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func paymentReturnPath(suffix string) string {
	return PaymentReturnURL(suffix)
}
