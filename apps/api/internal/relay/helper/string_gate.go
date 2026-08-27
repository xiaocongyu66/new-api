package helper

import (
	"github.com/QuantumNous/new-api/internal/gateway"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

const outputFilterWindowSize = gateway.OutputFilterWindowSize

// sensitiveOutputState is the request-level output gate state (stored on context).
type sensitiveOutputState = gateway.SensitiveOutputState

// outputFilterState gets/initializes request-level output detection state.
func outputFilterState(c contract.Context) *sensitiveOutputState {
	return gateway.OutputFilterState(c)
}

// outputChunkBlocked checks whether an output chunk should be blocked.
// Already-blocked streams discard directly (blocked=true); new chunks
// accumulate into the window before being checked. Target-domain hard gate
// is unconditional (independent of the sensitive-word toggle); the rest is
// governed by CheckSensitiveOnCompletionEnabled (default on).
func outputChunkBlocked(c contract.Context, data string) (bool, string) {
	return gateway.OutputChunkBlocked(c, data)
}

// terminateOutputSSE writes the termination frame to the client after a
// sensitive-output hit and marks the stream truncated. Writes an OpenAI-style
// content_filter terminal any-format client will break the
// stream. Idempotent: an already-written stream is not rewritten (subsequent
// chunks are dropped via blocked).
func terminateOutputSSE(c contract.Context) {
	gateway.TerminateOutputSSE(c)
}
