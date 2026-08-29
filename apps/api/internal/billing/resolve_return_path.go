package billing

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/egress/fetch_url"
)

// PaymentReturnURL computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(fetch_url.ServerAddress, "/")
	return base + suffix
}
