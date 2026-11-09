package billing

import (
	"strings"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
)

// RequestOutcome classifies how a request ended for request-count billing.
type RequestOutcome string

const (
	RequestOutcomeNormal RequestOutcome = "normal"
	RequestOutcomeEmpty  RequestOutcome = "empty"
	// RequestOutcomeClientAbort is a client-initiated disconnect: the user
	// abandoned the request, so it still consumes one request.
	RequestOutcomeClientAbort RequestOutcome = "client_abort"
	// RequestOutcomeUpstreamCutLong is an upstream drop after more than
	// RequestCountMinOutputTokens completion tokens: substantial output was
	// delivered, so it consumes one request.
	RequestOutcomeUpstreamCutLong RequestOutcome = "upstream_cut_long"
	// RequestOutcomeUpstreamCutShort is an upstream drop with too little output
	// to count as a delivered answer.
	RequestOutcomeUpstreamCutShort RequestOutcome = "upstream_cut_short"
	// RequestOutcomeFailed is a rejected request: it still settles its charge
	// but must not consume a request counter.
	RequestOutcomeFailed RequestOutcome = "failed"
)

// RequestCountMinOutputTokens is the upstream-cut boundary: more delivered
// completion tokens count as a served request, fewer do not.
const RequestCountMinOutputTokens = 1000

// ClassifyRequestOutcome maps the existing settlement signals (empty response,
// stream cut side, policy rejection) to how a request-kind subscription should
// count the request. Non-text formats and nil relay info never consume a count.
func ClassifyRequestOutcome(relayInfo *relaycommon.RelayInfo, completionTokens int, hasToolSurcharge bool, adminRejectReason string) RequestOutcome {
	if relayInfo == nil || !isTextGenerationFormat(relayInfo.RelayFormat) {
		return RequestOutcomeNormal
	}
	if strings.TrimSpace(adminRejectReason) != "" {
		return RequestOutcomeFailed
	}
	summary := textQuotaSummary{CompletionTokens: completionTokens}
	if hasToolSurcharge {
		summary.ToolSurchargeItems = []ToolSurchargeItem{{Count: 1}}
	}
	if isUndeliveredTextResponse(relayInfo, summary, adminRejectReason) {
		return RequestOutcomeEmpty
	}
	switch streamCutKind(relayInfo, completionTokens) {
	case streamCutClient:
		return RequestOutcomeClientAbort
	case streamCutUpstream:
		if completionTokens > RequestCountMinOutputTokens {
			return RequestOutcomeUpstreamCutLong
		}
		return RequestOutcomeUpstreamCutShort
	default:
		return RequestOutcomeNormal
	}
}

// CountsAsRequest reports whether this outcome consumes one request count.
func (o RequestOutcome) CountsAsRequest() bool {
	switch o {
	case RequestOutcomeNormal, RequestOutcomeClientAbort, RequestOutcomeUpstreamCutLong:
		return true
	default:
		return false
	}
}
