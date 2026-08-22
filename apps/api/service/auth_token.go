package service

import (
	"github.com/QuantumNous/new-api/internal/security/authtoken"
)

// The dashboard token mechanism (JWT issuing, parsing, and security proofs) now
// lives in internal/security/authtoken. It is a pure protocol concern with no
// model or database dependency, so it belongs with the security mechanisms
// rather than the identity business logic.
//
// These aliases keep the existing service.* call sites compiling while identity
// handlers migrate to the capability package. They are re-exports, not a second
// implementation.

// AuthIdentity is the server-validated identity attached to dashboard requests.
type AuthIdentity = authtoken.AuthIdentity

// Token lifetimes and replay window, shared with the session flows.
const (
	AccessTokenTTL      = authtoken.AccessTokenTTL
	SecurityProofTTL    = authtoken.SecurityProofTTL
	LoginSessionTTL     = authtoken.LoginSessionTTL
	RefreshReplayWindow = authtoken.RefreshReplayWindow
)

var (
	ErrAuthTokenInvalid = authtoken.ErrAuthTokenInvalid
	ErrAuthTokenExpired = authtoken.ErrAuthTokenExpired
	ErrProofScope       = authtoken.ErrProofScope
	ErrProofMethod      = authtoken.ErrProofMethod
)

// The forwarders below are functions rather than variables on purpose: a package
// variable holding a func can be reassigned at runtime, which would let any code
// in this package swap out token issuing or verification. Authentication
// primitives must not be rebindable, so each one is a wrapper.

// IssueAccessToken mints a dashboard access token.
func IssueAccessToken(identity AuthIdentity) (string, int64, error) {
	return authtoken.IssueAccessToken(identity)
}

// ParseAccessToken validates a dashboard access token.
func ParseAccessToken(raw string) (AuthIdentity, error) {
	return authtoken.ParseAccessToken(raw)
}

// ParseDashboardAccessToken distinguishes dashboard JWTs from opaque credentials.
func ParseDashboardAccessToken(raw string) (AuthIdentity, bool, error) {
	return authtoken.ParseDashboardAccessToken(raw)
}

// IssueSecurityProof mints a short-lived proof for sensitive operations.
func IssueSecurityProof(identity AuthIdentity, method string, scopes []string) (string, int64, error) {
	return authtoken.IssueSecurityProof(identity, method, scopes)
}

// VerifySecurityProof validates a proof against an identity and scope.
func VerifySecurityProof(raw string, identity AuthIdentity, requiredScope string, allowedMethods []string) (string, error) {
	return authtoken.VerifySecurityProof(raw, identity, requiredScope, allowedMethods)
}

// authSigningKey keeps the existing unexported call sites working; the exported
// derivation lives in the security package.
func authSigningKey(purpose string) []byte { return authtoken.SigningKey(purpose) }

const (
	authTokenIssuer   = authtoken.TokenIssuer
	authTokenAudience = authtoken.TokenAudience
)
