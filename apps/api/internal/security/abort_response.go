package security

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/httperr"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Both envelopes are shared with every other layer that can reject a request,
// so these wrappers forward to internal/transport/httperr instead of keeping a
// second copy of the response shape in this package.

func abortWithOpenAiMessage(c contract.Context, statusCode int, message string, code ...types.ErrorCode) {
	httperr.AbortWithOpenAiMessage(c, statusCode, message, code...)
}
