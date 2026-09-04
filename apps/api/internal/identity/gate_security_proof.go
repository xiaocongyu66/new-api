package identity

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/authtoken"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// RequireSecurityProof validates an X-Security-Proof header against the
// authenticated dashboard session and writes the shared proof error contract on
// failure, reporting whether the caller may proceed.
//
// This lives in the identity domain because it verifies an identity artifact:
// VerifySecurityProof and the ErrProof* sentinels are defined here. Having the
// transport middleware own it while this domain called back into it formed an
// import cycle once the auth session logic moved in.
func RequireSecurityProof(c contract.Context, requiredScope string, allowedMethods []string) bool {
	authID, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		writeSecurityProofError(c, "SECURITY_PROOF_INVALID", "安全验证状态无效")
		return false
	}
	raw := strings.TrimSpace(c.Header("X-Security-Proof"))
	if raw == "" {
		writeSecurityProofError(c, "SECURITY_PROOF_REQUIRED", "需要安全验证")
		return false
	}
	if _, err := VerifySecurityProof(raw, authID, requiredScope, allowedMethods); err != nil {
		switch {
		case errors.Is(err, authtoken.ErrAuthTokenExpired):
			writeSecurityProofError(c, "SECURITY_PROOF_EXPIRED", "安全验证已过期")
		case errors.Is(err, ErrProofScope):
			writeSecurityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", "安全验证范围不匹配")
		case errors.Is(err, ErrProofMethod):
			writeSecurityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", "安全验证方式不匹配")
		default:
			writeSecurityProofError(c, "SECURITY_PROOF_INVALID", "安全验证状态无效")
		}
		return false
	}
	return true
}

func writeSecurityProofError(c contract.Context, code, message string) {
	_ = c.JSON(http.StatusForbidden, common.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}
