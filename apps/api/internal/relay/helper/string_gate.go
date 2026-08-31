package helper

import (
	"github.com/QuantumNous/new-api/internal/gateway"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// sensitiveOutputState is the request-level output filter state (stored on context).
type sensitiveOutputState = gateway.SensitiveOutputState

// outputFilterState gets/initializes request-level output filter state.
func outputFilterState(c contract.Context) *sensitiveOutputState {
	return gateway.OutputFilterState(c)
}

// outputChunkFiltered filters one output chunk and returns the sanitized text
// that can be forwarded immediately. Target-domain filtering is unconditional;
// dictionary filtering is governed by CheckSensitiveOnCompletionEnabled.
func outputChunkFiltered(c contract.Context, data string) string {
	return gateway.OutputChunkFiltered(c, data)
}
