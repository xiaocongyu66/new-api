// Package httperr renders the request-rejection envelope shared by every layer
// that can abort a request: the security guards, the transport middleware and
// the relay entry points.
//
// It exists because those layers used to keep byte-identical copies of the same
// response shape. A change to the envelope had to be remembered in each copy,
// and a missed one meant clients saw a different error format depending on
// which layer rejected the call. The package sits above internal/logger on
// purpose: internal/common cannot host this helper because logger already
// imports common, and importing logger from common would close an import cycle.
package httperr

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// AbortWithOpenAiMessage rejects a request with the OpenAI-shaped error
// envelope every relay client parses, aborts the chain and records the reason
// against the caller's user id.
func AbortWithOpenAiMessage(c contract.Context, statusCode int, message string, code ...types.ErrorCode) {
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
