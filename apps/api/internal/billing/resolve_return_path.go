package billing

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/egress"
)

// PaymentReturnURL computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(egress.ServerAddress, "/")
	return base + suffix
}
