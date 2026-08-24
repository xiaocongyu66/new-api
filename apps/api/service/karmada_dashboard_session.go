package service

import (
	"time"

	"github.com/QuantumNous/new-api/internal/capabilities/integration"
)

const (
	KarmadaDashboardSessionTTL = 5 * time.Minute
	karmadaDashboardTokenUse   = "karmada_dashboard"
	karmadaDashboardAudience   = "new-api-karmada-dashboard"
)

var ErrKarmadaDashboardSessionInvalid = integration.ErrKarmadaDashboardSessionInvalid

// IssueKarmadaDashboardSession issues a short-lived JWT for Karmada dashboard access.
func IssueKarmadaDashboardSession(identity AuthIdentity) (string, int64, error) {
	return integration.IssueKarmadaDashboardSession(identity)
}

// ValidateKarmadaDashboardSession validates a Karmada dashboard session token.
func ValidateKarmadaDashboardSession(raw string) (AuthIdentity, error) {
	return integration.ValidateKarmadaDashboardSession(raw)
}