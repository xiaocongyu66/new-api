package price_expression_test

import (
	"testing"

	priceexpr "github.com/QuantumNous/new-api/internal/billing/price_expression"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Production expressions price each context tier as a flat per-request fee:
// `tier("t1", fixed(0.055))` must settle to exactly $0.055 × group ratio,
// whatever the token counts inside that tier.
const fixedLadderExpr = `len <= 10000 ? tier("t1", fixed(0.055))
: len <= 100000 ? tier("t2", fixed(0.11))
: tier("t3", fixed(0.42))`

func TestComputeTieredQuota_FixedChargesFlatAmountPerTier(t *testing.T) {
	snap := &priceexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   fixedLadderExpr,
		ExprHash:     priceexpr.ExprHashString(fixedLadderExpr),
		GroupRatio:   0.5,
		QuotaPerUnit: 500_000,
	}

	tests := []struct {
		name      string
		params    priceexpr.TokenParams
		wantTier  string
		wantQuota int
	}{
		// $0.055 × 500000 quota/USD × 0.5 group ratio
		{"short context", priceexpr.TokenParams{P: 3000, C: 800, Len: 3000}, "t1", 13_750},
		{"same tier ignores token counts", priceexpr.TokenParams{P: 9000, C: 40_000, Len: 9000}, "t1", 13_750},
		{"tier boundary is inclusive", priceexpr.TokenParams{P: 10_000, Len: 10_000}, "t1", 13_750},
		{"middle tier", priceexpr.TokenParams{P: 50_000, C: 10, Len: 50_000}, "t2", 27_500},
		{"catch-all tier", priceexpr.TokenParams{P: 900_000, Len: 900_000}, "t3", 105_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := priceexpr.ComputeTieredQuota(snap, tt.params)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTier, result.MatchedTier)
			assert.Equal(t, tt.wantQuota, result.ActualQuotaAfterGroup)
			assert.Nil(t, result.Clamp)
		})
	}
}

// fixed() is a plain term of the expression, so it composes with per-token
// prices in the same tier (a per-request fee on top of token pricing).
func TestComputeTieredQuota_FixedComposesWithTokenPrices(t *testing.T) {
	exprStr := `tier("base", fixed(0.01) + p * 2 + c * 8)`
	snap := &priceexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   exprStr,
		ExprHash:     priceexpr.ExprHashString(exprStr),
		GroupRatio:   1,
		QuotaPerUnit: 500_000,
	}

	// $0.01 + 1000 × $2/1M + 500 × $8/1M = $0.016 → 8000 quota
	result, err := priceexpr.ComputeTieredQuota(snap, priceexpr.TokenParams{P: 1000, C: 500, Len: 1000})
	require.NoError(t, err)
	assert.Equal(t, 8_000, result.ActualQuotaAfterGroup)
}
