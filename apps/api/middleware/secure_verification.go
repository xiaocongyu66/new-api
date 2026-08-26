package middleware

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/service"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their controller.
func SecureVerificationRequired() contract.Middleware {
	return func(c contract.Context) {
		if !RequireSecurityProof(c, "channel.key.read", []string{"2fa", "passkey"}) {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}

// RequireSecurityProof validates a proof against the authenticated dashboard
// session and writes the shared proof error contract on failure.
func RequireSecurityProof(c contract.Context, requiredScope string, allowedMethods []string) bool {
	identity, ok := security.GetSessionAuthIdentity(c)
	if !ok {
		securityProofError(c, "SECURITY_PROOF_INVALID", "安全验证状态无效")
		return false
	}
	raw := strings.TrimSpace(c.Header("X-Security-Proof"))
	if raw == "" {
		securityProofError(c, "SECURITY_PROOF_REQUIRED", "需要安全验证")
		return false
	}
	if _, err := service.VerifySecurityProof(raw, identity, requiredScope, allowedMethods); err != nil {
		switch {
		case errors.Is(err, service.ErrAuthTokenExpired):
			securityProofError(c, "SECURITY_PROOF_EXPIRED", "安全验证已过期")
		case errors.Is(err, service.ErrProofScope):
			securityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", "安全验证范围不匹配")
		case errors.Is(err, service.ErrProofMethod):
			securityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", "安全验证方式不匹配")
		default:
			securityProofError(c, "SECURITY_PROOF_INVALID", "安全验证状态无效")
		}
		return false
	}
	return true
}

func securityProofError(c contract.Context, code, message string) {
	_ = c.JSON(http.StatusForbidden, common.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}
