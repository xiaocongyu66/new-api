package identity

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

// AuthIdentity aliases the service-level identity carried through the
// dashboard auth chain.
type AuthIdentity = service.AuthIdentity

func abortWithOpenAiMessage(c contract.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	_ = c.JSON(statusCode, common.H{
		"error": common.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(c.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func isAllowedSecurityProofScope(scope string) bool {
	switch scope {
	case SecurityProofScopeChannelKeyRead, SecurityProofScopePasskeyRegister, SecurityProofScopePasskeyDelete:
		return true
	default:
		return false
	}
}

const (
	secureVerificationMethod2FA     = "2fa"
	secureVerificationMethodPasskey = "passkey"
)
