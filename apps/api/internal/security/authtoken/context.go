package authtoken

import "github.com/QuantumNous/new-api/internal/transport/contract"

// AuthIdentityContextKey is where the dashboard auth chain stores the resolved
// AuthIdentity on the request context.
const AuthIdentityContextKey = "auth_identity"

// ReadAuthIdentity returns the AuthIdentity the auth chain stored on the
// request, if any. PAT-authenticated requests intentionally have no SessionID
// and so cannot manage browser sessions.
//
// This lives beside AuthIdentity rather than in internal/security because
// reading it needs nothing but the request context: keeping it here lets the
// identity domain resolve the current caller without importing security, which
// would otherwise cycle once security consumes identity's records.
func ReadAuthIdentity(c contract.Context) (AuthIdentity, bool) {
	value, ok := c.Get(AuthIdentityContextKey)
	if !ok {
		return AuthIdentity{}, false
	}
	identity, ok := value.(AuthIdentity)
	return identity, ok
}

// ReadSessionAuthIdentity returns only identities backed by a live dashboard
// session, falling back to the discrete context values the auth chain sets.
// PAT-authenticated requests intentionally fail this check.
func ReadSessionAuthIdentity(c contract.Context) (AuthIdentity, bool) {
	identity, ok := ReadAuthIdentity(c)
	if !ok {
		identity = AuthIdentity{
			UserID:          c.GetInt("id"),
			SessionID:       c.GetString("session_id"),
			UserAuthVersion: c.GetInt64("auth_version"),
			SessionVersion:  c.GetInt64("session_version"),
		}
	}
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return AuthIdentity{}, false
	}
	return identity, true
}
