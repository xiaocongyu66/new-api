package service

import (
	"github.com/QuantumNous/new-api/internal/security/authtoken"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKarmadaDashboardSessionPurposeIsolation(t *testing.T) {
	useTestSessionSecret(t)
	authID := authtoken.AuthIdentity{UserID: 42, SessionID: "session-1", UserAuthVersion: 3, SessionVersion: 2}

	token, expiresAt, err := IssueKarmadaDashboardSession(authID)
	require.NoError(t, err)
	assert.Positive(t, expiresAt)

	access, _, err := authtoken.IssueAccessToken(authID)
	require.NoError(t, err)
	_, err = ValidateKarmadaDashboardSession(access)
	assert.ErrorIs(t, err, ErrKarmadaDashboardSessionInvalid)

	_, err = ValidateKarmadaDashboardSession(token)
	assert.NotErrorIs(t, err, ErrKarmadaDashboardSessionInvalid)
}

func TestKarmadaDashboardSessionRejectsTampering(t *testing.T) {
	useTestSessionSecret(t)
	authID := authtoken.AuthIdentity{UserID: 42, SessionID: "session-1", UserAuthVersion: 3, SessionVersion: 2}
	token, _, err := IssueKarmadaDashboardSession(authID)
	require.NoError(t, err)

	tamperAt := len(token) - 2
	replacement := "x"
	if token[tamperAt] == 'x' {
		replacement = "y"
	}
	tampered := token[:tamperAt] + replacement + token[tamperAt+1:]
	_, err = ValidateKarmadaDashboardSession(tampered)
	assert.ErrorIs(t, err, ErrKarmadaDashboardSessionInvalid)
}
