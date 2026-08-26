package authtoken

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseAccessTokenPreservesUnderlyingJWTError asserts a rejected token wraps
// both the package sentinel and the underlying jwt error.
//
// Callers branch on the sentinel to decide the HTTP response, and need the jwt
// error to distinguish a tampered signature from a malformed token when logging
// an authentication failure. Formatting the cause with %v instead of %w would
// silently break every errors.Is check against jwt.ErrToken*.
func TestParseAccessTokenPreservesUnderlyingJWTError(t *testing.T) {
	useTestSessionSecret(t)

	identity := AuthIdentity{UserID: 7, SessionID: "session-err", UserAuthVersion: 1, SessionVersion: 1}
	token, _, err := IssueAccessToken(identity)
	require.NoError(t, err)

	tampered := tamperFinalTokenByte(token)

	_, err = ParseAccessToken(tampered)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthTokenInvalid, "sentinel drives the rejection response")
	assert.ErrorIs(t, err, jwt.ErrSignatureInvalid, "underlying jwt cause must stay inspectable")
}

// TestParseAccessTokenRejectsMalformedTokenWithInspectableCause asserts a
// structurally invalid token also keeps its jwt cause.
func TestParseAccessTokenRejectsMalformedTokenWithInspectableCause(t *testing.T) {
	useTestSessionSecret(t)

	_, err := ParseAccessToken("not-a-jwt")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthTokenInvalid)
	assert.ErrorIs(t, err, jwt.ErrTokenMalformed)
}

// tamperFinalTokenByte flips the second-to-last signature character,
// producing a token with valid structure but an invalid signature. The last
// base64url char may carry only ignored padding bits in unpadded JWT
// segments, so flipping it can leave the decoded bytes — and the verified
// signature — untouched; the second-to-last char is always fully significant.
func tamperFinalTokenByte(token string) string {
	if len(token) < 2 {
		return token
	}
	i := len(token) - 2
	replacement := byte('x')
	if token[i] == 'x' {
		replacement = 'y'
	}
	return token[:i] + string(replacement) + token[i+1:]
}
