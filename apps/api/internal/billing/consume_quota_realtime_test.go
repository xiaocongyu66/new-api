package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalculateAudioQuotaFixedPriceChargesModelPriceTimesGroupRatio pins the
// per-request pricing branch used by /v1/audio/* and realtime (wss) billing.
// A zero ModelPrice here is what silently made every priced audio model free
// when the callers forgot to copy relayInfo.PriceData.ModelPrice into
// QuotaInfo.
func TestCalculateAudioQuotaFixedPriceChargesModelPriceTimesGroupRatio(t *testing.T) {
	testCases := []struct {
		name       string
		modelPrice float64
		groupRatio float64
		wantQuota  int
	}{
		{"priced model bills per call", 0.1, 1, 50000},
		{"group ratio scales the charge", 0.1, 0.1, 5000},
		{"zero price stays free", 0, 1, 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			quota, clamp := calculateAudioQuota(QuotaInfo{
				ModelName:  "grok-voice-latest",
				UsePrice:   true,
				ModelPrice: tc.modelPrice,
				GroupRatio: tc.groupRatio,
				InputDetails: TokenDetails{
					TextTokens: 17,
				},
				OutputDetails: TokenDetails{
					AudioTokens: 67,
				},
			})
			require.Equal(t, tc.wantQuota, quota)
			assert.Nil(t, clamp)
		})
	}
}
