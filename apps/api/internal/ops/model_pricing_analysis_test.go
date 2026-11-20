package ops

import (
	"strconv"
	"testing"

	billing "github.com/QuantumNous/new-api/internal/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTieredWindowCostIsExact pins the per-window cost formula used by
// channelWindows: each consume log is priced at its real prompt/completion
// tokens, not at the representative 1.25x context used for display.
func TestTieredWindowCostIsExact(t *testing.T) {
	const expr = `tier("base", p * 2 + c * 8)`

	samples := []pricingSample{{Prompt: 1000, Completion: 500, Quota: 1}}
	assert.InDelta(t, (2*1000+8*500)/1e6, windowCostUSD(expr, samples), 1e-12)

	// A different completion ratio must not incur the old 1.25x inflation.
	samples = []pricingSample{{Prompt: 1000, Completion: 2000, Quota: 1}}
	assert.InDelta(t, (2*1000+8*2000)/1e6, windowCostUSD(expr, samples), 1e-12)

	// Multiple logs sum to the sum of their exact costs.
	samples = []pricingSample{
		{Prompt: 1000, Completion: 500, Quota: 1},
		{Prompt: 2000, Completion: 100, Quota: 2},
	}
	want := (2*1000+8*500)/1e6 + (2*2000+8*100)/1e6
	assert.InDelta(t, want, windowCostUSD(expr, samples), 1e-12)

	// No expression means no upstream cost.
	assert.Zero(t, windowCostUSD("", samples))

	// evalUsd remains the representative-context price used for display:
	// P=ctx, C=ctx/4. It must not be reused as the exact per-request price.
	eval := evalUsd(expr, 1000)
	require.NotNil(t, eval)
	assert.InDelta(t, 4.0*1000/1e6, *eval, 1e-12)
	assert.NotEqual(t, (2*1000+8*500)/1e6, *eval)
}

func TestPricingModeHelpers(t *testing.T) {
	const perTokenExpr = `tier("base", p * 2 + c * 8)`
	tiered := map[string]any{
		billing.BillingModeField: billing.BillingModeTieredExpr,
		billing.BillingExprField: perTokenExpr,
	}
	assert.Equal(t, "per_token_tiered", priceModeFromFields(perTokenExpr, tiered))
	assert.Equal(t, billing.BillingModeTieredExpr, billingModeFromFields(tiered))

	const perCallExpr = `len <= 10000 ? tier("t1", fixed(0.055)) : tier("t2", fixed(0.11))`
	fixedTiered := map[string]any{
		billing.BillingModeField: billing.BillingModeTieredExpr,
		billing.BillingExprField: perCallExpr,
	}
	assert.Equal(t, "per_call_tiered", priceModeFromFields(perCallExpr, fixedTiered))
	assert.Equal(t, billing.BillingModeTieredExpr, billingModeFromFields(fixedTiered))

	modelPrice := map[string]any{"model_price": 0.38}
	assert.Equal(t, "per_call", priceModeFromFields("", modelPrice))
	assert.Equal(t, "per-request", billingModeFromFields(modelPrice))

	modelRatio := map[string]any{"model_ratio": 0.002, "completion_ratio": 1.0}
	assert.Equal(t, "per_token", priceModeFromFields("", modelRatio))
	assert.Equal(t, "per-token", billingModeFromFields(modelRatio))

	assert.Empty(t, priceModeFromFields("", nil))
	assert.Empty(t, priceModeFromFields("", map[string]any{}))
	assert.Empty(t, billingModeFromFields(nil))
	assert.Empty(t, billingModeFromFields(map[string]any{}))
}

func TestChunkStrings(t *testing.T) {
	items := make([]string, 1200)
	for i := range items {
		items[i] = strconv.Itoa(i)
	}
	chunks := chunkStrings(items, 500)
	require.Len(t, chunks, 3)
	assert.Len(t, chunks[0], 500)
	assert.Len(t, chunks[1], 500)
	assert.Len(t, chunks[2], 200)
	assert.Equal(t, "0", chunks[0][0])
	assert.Equal(t, "499", chunks[0][499])
	assert.Equal(t, "500", chunks[1][0])
	assert.Equal(t, "999", chunks[1][499])
	assert.Equal(t, "1000", chunks[2][0])
	assert.Equal(t, "1199", chunks[2][199])
	assert.Nil(t, chunkStrings(nil, 500))
	assert.Nil(t, chunkStrings([]string{}, 500))
	assert.Equal(t, items, chunkStrings(items, len(items))[0])
}

func TestResolveUpstreamFields(t *testing.T) {
	const field = billing.BillingExprField
	sourceModels := map[string]map[string]any{
		"[套餐]x":  {field: "exact-bracket"},
		"x":      {field: "stripped"},
		"mapped": {field: "via-mapping"},
		"empty":  {},
	}

	// Exact match wins over every fallback.
	got := resolveUpstreamFields("[套餐]x", sourceModels, nil, map[string]string{"[套餐]x": "mapped"})
	require.NotNil(t, got)
	assert.Equal(t, "exact-bracket", got[field])

	// [套餐] prefix falls back to the bare name.
	got = resolveUpstreamFields("[套餐]y", map[string]map[string]any{"y": {field: "bare-y"}}, nil, nil)
	require.NotNil(t, got)
	assert.Equal(t, "bare-y", got[field])

	// a/ prefix falls back to the bare name.
	got = resolveUpstreamFields("a/z", map[string]map[string]any{"z": {field: "bare-z"}}, nil, nil)
	require.NotNil(t, got)
	assert.Equal(t, "bare-z", got[field])

	// model_mapping supplies the upstream name when no snapshot name matches.
	got = resolveUpstreamFields("pkg-gemini", sourceModels, nil, map[string]string{"pkg-gemini": "mapped"})
	require.NotNil(t, got)
	assert.Equal(t, "via-mapping", got[field])

	// Empty field maps are treated as misses.
	assert.Nil(t, resolveUpstreamFields("empty", sourceModels, nil, nil))

	// Reference fallback uses the same candidate order.
	reference := map[string]map[string]any{"x": {field: "reference-x"}}
	got = resolveUpstreamFields("[套餐]x", nil, reference, nil)
	require.NotNil(t, got)
	assert.Equal(t, "reference-x", got[field])

	// Nothing matches -> nil.
	assert.Nil(t, resolveUpstreamFields("ghost", sourceModels, nil, nil))
}
