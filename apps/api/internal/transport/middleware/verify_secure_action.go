package middleware

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their own handler.
//
// The verification itself lives in the identity domain, which owns the proof
// artifact; this is only the route-level wrapper.
func SecureVerificationRequired() contract.Middleware {
	return func(c contract.Context) {
		if !identity.RequireSecurityProof(c, "channel.key.read", []string{"2fa", "passkey"}) {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}
