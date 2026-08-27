package ops

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/authtoken"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// AuthIdentity aliases the service-level identity carried through the
// dashboard auth chain.
type AuthIdentity = authtoken.AuthIdentity

// AbortWithMessage writes a JSON error response and aborts the request.
func AbortWithMessage(c contract.Context, statusCode int, message string) {
	_ = c.JSON(statusCode, common.H{
		"error": common.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
		},
	})
	c.Abort()
}
