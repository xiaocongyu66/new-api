package service

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
)

// useTestSessionSecret pins a deterministic signing secret so token derivation is
// reproducible across the karmada dashboard session tests.
//
// The identity package keeps its own copy rather than either package exporting a
// test-only helper.
func useTestSessionSecret(t *testing.T) {
	t.Helper()
	previous := common.SessionSecret
	common.SessionSecret = "test-session-secret-with-sufficient-entropy"
	t.Cleanup(func() { common.SessionSecret = previous })
}
