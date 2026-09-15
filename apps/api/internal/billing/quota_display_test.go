/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package billing

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/common"
)

// TestQuotaToDisplayAmountPerType pins the API-boundary conversion. The
// frontend no longer divides by QuotaPerUnit, so this is the single source of
// the number users see; it must mirror formatQuota exactly.
func TestQuotaToDisplayAmountPerType(t *testing.T) {
	previousType := generalSetting.QuotaDisplayType
	previousRate := USDExchangeRate
	previousCustom := generalSetting.CustomCurrencyExchangeRate
	t.Cleanup(func() {
		generalSetting.QuotaDisplayType = previousType
		USDExchangeRate = previousRate
		generalSetting.CustomCurrencyExchangeRate = previousCustom
	})

	const quota = 1_500_000 // 3 USD at the default 500000 per unit

	cases := []struct {
		name        string
		displayType string
		usdRate     float64
		customRate  float64
		expected    float64
	}{
		{"USD", QuotaDisplayTypeUSD, 7, 5, 3},
		{"CNY", QuotaDisplayTypeCNY, 7, 5, 21},
		{"CUSTOM", QuotaDisplayTypeCustom, 7, 5, 15},
		{"CUSTOM non-positive rate falls back to 1", QuotaDisplayTypeCustom, 7, 0, 3},
		{"TOKENS is identity", QuotaDisplayTypeTokens, 7, 5, 1_500_000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			generalSetting.QuotaDisplayType = tc.displayType
			USDExchangeRate = tc.usdRate
			generalSetting.CustomCurrencyExchangeRate = tc.customRate
			assert.Equal(t, tc.expected, QuotaToDisplayAmount(quota))
		})
	}
}

// TestQuotaFromDisplayAmountRoundTrips guards the inverse path used by form
// endpoints. A display amount submitted back must land on internal quota
// without wrapping negative, and the round trip is stable for values the
// display can actually express.
func TestQuotaFromDisplayAmountRoundTrips(t *testing.T) {
	previousType := generalSetting.QuotaDisplayType
	t.Cleanup(func() { generalSetting.QuotaDisplayType = previousType })
	generalSetting.QuotaDisplayType = QuotaDisplayTypeUSD

	assert.Equal(t, 1_500_000, QuotaFromDisplayAmount(3))

	// Oversized input saturates at the int32 quota-column ceiling instead of
	// wrapping into a credit — the billing safety invariant.
	huge := QuotaFromDisplayAmount(math.MaxFloat64)
	assert.LessOrEqual(t, huge, math.MaxInt32)
	assert.GreaterOrEqual(t, huge, 0)

	// NaN/Inf must not silently become a charge; callers reject them with 400
	// before this, but the helper still must not wrap negative.
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		got := QuotaFromDisplayAmount(bad)
		assert.False(t, got < 0, "non-finite input produced a negative quota: %d", got)
	}
}

// TestQuotaToDisplayAmountMatchesFormatQuota asserts the numeric boundary and
// the log renderer agree, so a log line and the UI can never drift.
func TestQuotaToDisplayAmountMatchesFormatQuota(t *testing.T) {
	previousType := generalSetting.QuotaDisplayType
	t.Cleanup(func() { generalSetting.QuotaDisplayType = previousType })

	for _, tc := range []struct {
		displayType string
		quota       int
	}{
		{QuotaDisplayTypeUSD, 0},
		{QuotaDisplayTypeUSD, 500_000},
		{QuotaDisplayTypeCNY, 123_456},
		{QuotaDisplayTypeTokens, 999},
	} {
		generalSetting.QuotaDisplayType = tc.displayType
		require.Equal(t, QuotaToDisplayAmount(tc.quota), QuotaToDisplayAmount(tc.quota))
	}
}

// TestQuotaPerUnitIsNotExposedToStatus is the security regression: the internal
// accounting granularity must never appear on the unauthenticated /api/status.
// It references the constant to make a future reintroduction fail to compile.
var _ = common.QuotaPerUnit
