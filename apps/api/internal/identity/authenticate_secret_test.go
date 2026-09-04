package identity

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
)

// useTestSessionSecret pins a deterministic signing secret for auth tests.
//
// It lives here because the session and external-dashboard flows in this package
// sign tokens through the shared derivation; the token mechanism package keeps
// its own copy so neither package exports a test-only helper.
func useTestSessionSecret(t *testing.T) {
	t.Helper()
	previous := common.SessionSecret
	common.SessionSecret = "test-session-secret-with-sufficient-entropy"
	t.Cleanup(func() { common.SessionSecret = previous })
}
