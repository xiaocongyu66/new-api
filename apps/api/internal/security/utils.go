package security

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
)

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

func abortWithMidjourneyMessage(c contract.Context, statusCode int, code int, description string) {
	_ = c.JSON(statusCode, common.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Context(), description)
}
