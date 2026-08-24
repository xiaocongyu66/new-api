package service

import (
	"github.com/QuantumNous/new-api/internal/capabilities/integration"
)

// PaymentReturnURL computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func PaymentReturnURL(suffix string) string {
	return integration.PaymentReturnURL(suffix)
}