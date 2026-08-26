package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// PaymentReturnURL computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	return base + suffix
}
