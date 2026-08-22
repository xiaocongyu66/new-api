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

var (
	// IssueAccessToken mints a dashboard access token.
	IssueAccessToken = authtoken.IssueAccessToken
	// ParseAccessToken validates a dashboard access token.
	ParseAccessToken = authtoken.ParseAccessToken
	// ParseDashboardAccessToken distinguishes dashboard JWTs from opaque credentials.
	ParseDashboardAccessToken = authtoken.ParseDashboardAccessToken
	// IssueSecurityProof mints a short-lived proof for sensitive operations.
	IssueSecurityProof = authtoken.IssueSecurityProof
	// VerifySecurityProof validates a proof against an identity and scope.
	VerifySecurityProof = authtoken.VerifySecurityProof
)

// authSigningKey keeps the existing unexported call sites working; the exported
// derivation lives in the security package.
func authSigningKey(purpose string) []byte { return authtoken.SigningKey(purpose) }

const (
	authTokenIssuer   = authtoken.TokenIssuer
	authTokenAudience = authtoken.TokenAudience
)
