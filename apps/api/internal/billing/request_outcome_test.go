package billing

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func TestClassifyRequestOutcome(t *testing.T) {
	newRelayInfo := func(format types.RelayFormat) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:  format,
			StreamStatus: relaycommon.NewStreamStatus(),
		}
	}
	upstreamCut := func(format types.RelayFormat, tokens int) *relaycommon.RelayInfo {
		info := newRelayInfo(format)
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, nil)
		return info
	}
	clientAbort := func(format types.RelayFormat) *relaycommon.RelayInfo {
		info := newRelayInfo(format)
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
		return info
	}

	cases := []struct {
		name             string
		relayInfo        *relaycommon.RelayInfo
		completionTokens int
		hasToolSurcharge bool
		adminReject      string
		want             RequestOutcome
	}{
		{
			name:      "nil relay info is normal",
			relayInfo: nil,
			want:      RequestOutcomeNormal,
		},
		{
			name:      "non-text format is normal even with zero completion",
			relayInfo: newRelayInfo(types.RelayFormatOpenAIImage),
			want:      RequestOutcomeNormal,
		},
		{
			name:             "text request with output is normal",
			relayInfo:        newRelayInfo(types.RelayFormatOpenAI),
			completionTokens: 120,
			want:             RequestOutcomeNormal,
		},
		{
			name:      "zero completion without observed content is empty",
			relayInfo: newRelayInfo(types.RelayFormatOpenAI),
			want:      RequestOutcomeEmpty,
		},
		{
			name: "observed stream without deliverable is empty",
			relayInfo: func() *relaycommon.RelayInfo {
				info := newRelayInfo(types.RelayFormatOpenAI)
				info.StreamContentObserved = true
				return info
			}(),
			want: RequestOutcomeEmpty,
		},
		{
			name: "observed stream with text is normal",
			relayInfo: func() *relaycommon.RelayInfo {
				info := newRelayInfo(types.RelayFormatOpenAI)
				info.StreamContentObserved = true
				info.StreamSawTextContent = true
				return info
			}(),
			completionTokens: 20,
			want:             RequestOutcomeNormal,
		},
		{
			name:             "tool surcharge prevents empty classification",
			relayInfo:        newRelayInfo(types.RelayFormatOpenAI),
			hasToolSurcharge: true,
			want:             RequestOutcomeNormal,
		},
		{
			name:        "admin rejection is failed",
			relayInfo:   newRelayInfo(types.RelayFormatOpenAI),
			adminReject: "policy violation",
			want:        RequestOutcomeFailed,
		},
		{
			name:             "client abort counts",
			relayInfo:        clientAbort(types.RelayFormatOpenAI),
			completionTokens: 400,
			want:             RequestOutcomeClientAbort,
		},
		{
			name:             "long upstream cut counts",
			relayInfo:        upstreamCut(types.RelayFormatClaude, 1500),
			completionTokens: 1500,
			want:             RequestOutcomeUpstreamCutLong,
		},
		{
			name:             "short upstream cut does not count",
			relayInfo:        upstreamCut(types.RelayFormatClaude, 500),
			completionTokens: 500,
			want:             RequestOutcomeUpstreamCutShort,
		},
		{
			name:             "upstream cut at the boundary does not count",
			relayInfo:        upstreamCut(types.RelayFormatClaude, RequestCountMinOutputTokens),
			completionTokens: RequestCountMinOutputTokens,
			want:             RequestOutcomeUpstreamCutShort,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRequestOutcome(tc.relayInfo, tc.completionTokens, tc.hasToolSurcharge, tc.adminReject)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRequestOutcomeCountsAsRequest(t *testing.T) {
	counted := []RequestOutcome{
		RequestOutcomeNormal,
		RequestOutcomeClientAbort,
		RequestOutcomeUpstreamCutLong,
	}
	waived := []RequestOutcome{
		RequestOutcomeEmpty,
		RequestOutcomeUpstreamCutShort,
		RequestOutcomeFailed,
	}
	for _, outcome := range counted {
		assert.True(t, outcome.CountsAsRequest(), "outcome %q must count", outcome)
	}
	for _, outcome := range waived {
		assert.False(t, outcome.CountsAsRequest(), "outcome %q must be waived", outcome)
	}
}
