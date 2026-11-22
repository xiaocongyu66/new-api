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

	samples := []pricingSample{{Prompt: 1000, Completion: 500, InputLen: 1000, Quota: 1}}
	assert.InDelta(t, (2*1000+8*500)/1e6, windowCostUSD(expr, samples), 1e-12)

	// A different completion ratio must not incur the old 1.25x inflation.
	samples = []pricingSample{{Prompt: 1000, Completion: 2000, InputLen: 1000, Quota: 1}}
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

	// A channel mapping wins over the exact-name catalog entry.
	got := resolveUpstreamFields("[套餐]x", sourceModels, map[string]string{"[套餐]x": "mapped"})
	require.NotNil(t, got)
	assert.Equal(t, "via-mapping", got[field])

	// Without a mapping the exact name wins over every strip fallback.
	got = resolveUpstreamFields("[套餐]x", sourceModels, nil)
	require.NotNil(t, got)
	assert.Equal(t, "exact-bracket", got[field])

	// [套餐] prefix falls back to the bare name.
	got = resolveUpstreamFields("[套餐]y", map[string]map[string]any{"y": {field: "bare-y"}}, nil)
	require.NotNil(t, got)
	assert.Equal(t, "bare-y", got[field])

	// a/ prefix falls back to the bare name.
	got = resolveUpstreamFields("a/z", map[string]map[string]any{"z": {field: "bare-z"}}, nil)
	require.NotNil(t, got)
	assert.Equal(t, "bare-z", got[field])

	// [次] suffix falls back to the bare name.
	got = resolveUpstreamFields("claude-opus-5[次]", map[string]map[string]any{"claude-opus-5": {field: "bare-call"}}, nil)
	require.NotNil(t, got)
	assert.Equal(t, "bare-call", got[field])

	// model_mapping outranks prefix strips: it is the admin's explicit
	// statement of what this channel actually serves.
	got = resolveUpstreamFields("pkg-gemini", sourceModels, map[string]string{"pkg-gemini": "mapped"})
	require.NotNil(t, got)
	assert.Equal(t, "via-mapping", got[field])

	// Mapping wins over the stripped name when both exist.
	got = resolveUpstreamFields("[套餐]x", map[string]map[string]any{
		"x":      {field: "stripped"},
		"mapped": {field: "via-mapping"},
	}, map[string]string{"[套餐]x": "mapped"})
	require.NotNil(t, got)
	assert.Equal(t, "via-mapping", got[field])

	// Mapping wins even when the local name itself exists upstream: the
	// catalog row is a different product than the one this channel proxies.
	got = resolveUpstreamFields("gemini-4-hydrogen", map[string]map[string]any{
		"gemini-4-hydrogen": {field: "catalog-row"},
		"哈基米4h":             {field: "mapped-call"},
	}, map[string]string{"gemini-4-hydrogen": "哈基米4h"})
	require.NotNil(t, got)
	assert.Equal(t, "mapped-call", got[field])

	// Empty field maps are treated as misses.
	assert.Nil(t, resolveUpstreamFields("empty", sourceModels, nil))

	// Nothing matches -> nil: the channel stays unpriced instead of borrowing
	// a reference source's prices.
	assert.Nil(t, resolveUpstreamFields("ghost", sourceModels, nil))
}

// TestWindowCostUsesSettlementInputLen pins the tier-context fix: per-call
// tiers must resolve against the input length settlement used (Claude-format
// responses report prompt tokens excluding cache), not plain prompt tokens.
func TestWindowCostUsesSettlementInputLen(t *testing.T) {
	const expr = `len <= 10000 ? tier("t1", fixed(0.055)) : tier("t2", fixed(0.11))`

	// 9000 prompt + 2000 cache tokens bills at the 11k tier ($0.11).
	withCache := []pricingSample{{Prompt: 9000, Completion: 10, InputLen: 11000, Quota: 1}}
	assert.InDelta(t, 0.11, windowCostUSD(expr, withCache), 1e-12)

	// Under 10k context the same request bills the cheap tier.
	noCache := []pricingSample{{Prompt: 9000, Completion: 10, InputLen: 9000, Quota: 1}}
	assert.InDelta(t, 0.055, windowCostUSD(expr, noCache), 1e-12)
}

// TestTieredExprIgnoresStaleExprForConcreteMode pins that a leftover
// billing_expr cannot override a stored non-tiered mode: the relay bills from
// ModelPrice/ModelRatio in that case, so the comparison must too.
func TestTieredExprIgnoresStaleExprForConcreteMode(t *testing.T) {
	const stale = `p * 5 + c * 25`

	if expr, ok := tieredExpr("per-request", stale, "", ""); ok {
		t.Fatalf("stale expr must be ignored for per-request mode, got %q", expr)
	}
	if expr, ok := tieredExpr("per-token", stale, "", ""); ok {
		t.Fatalf("stale expr must be ignored for per-token mode, got %q", expr)
	}

	// A model with no stored mode keeps the legacy expression fallback.
	if expr, ok := tieredExpr("", stale, "", ""); !ok || expr != stale {
		t.Fatalf("bare expression fallback lost: %q ok=%v", expr, ok)
	}
}

// TestIsPresetSource covers the reference-preset filter used by
// latestSnapshotValues.
func TestIsPresetSource(t *testing.T) {
	assert.True(t, isPresetSource("models.dev 价格预设(-101)"))
	assert.True(t, isPresetSource("官方倍率预设(-100)"))
	assert.False(t, isPresetSource("顺达 抗截断 按次(15)"))
	assert.False(t, isPresetSource(""))
}

// TestManualPriceFieldsShape pins the two manual price shapes: per-request
// becomes a fixed ModelPrice, per-token becomes the same tier() expression the
// evaluator already prices, with USD/1M coefficients used as-is.
func TestManualPriceFieldsShape(t *testing.T) {
	perCall := manualPriceFields(&ManualUpstreamPrice{
		ChannelID: 7, ModelName: "gpt-4o", Mode: manualModePerCall, PriceIn: 0.02,
	})
	expr, ok := modelExpr(perCall)
	require.True(t, ok)
	assert.Equal(t, "per_call", priceModeFromFields(expr, perCall))
	price := evalUsd(expr, 1000)
	require.NotNil(t, price)
	assert.InDelta(t, 0.02, *price, 1e-12)

	perToken := manualPriceFields(&ManualUpstreamPrice{
		ChannelID: 7, ModelName: "claude", Mode: manualModePerToken, PriceIn: 3, PriceOut: 15,
	})
	expr, ok = modelExpr(perToken)
	require.True(t, ok)
	assert.Equal(t, "per_token_tiered", priceModeFromFields(expr, perToken))
	price = evalUsd(expr, 1000)
	require.NotNil(t, price)
	// 1k input + 250 output at USD 3 / 15 per 1M.
	assert.InDelta(t, (3*1000+15*250)/1e6, *price, 1e-12)
}

// TestBuildChannelEntryFallsBackToManualPrice pins the precedence rule: a
// fetched snapshot wins, a manual entry is used only when the channel has no
// snapshot for that model, and the row is marked price_source=manual.
func TestBuildChannelEntryFallsBackToManualPrice(t *testing.T) {
	ch := boundChannelRow{ChannelID: 7, ChannelName: "no-pricing-upstream", ModelName: "gpt-4o"}
	ug := usageAgg{ChannelID: 7, ModelName: "gpt-4o", Tokens: 1000, Requests: 2, Quota: 0}
	manual := map[[2]any]map[string]any{
		manualPriceKey(7, "gpt-4o"): manualPriceFields(&ManualUpstreamPrice{
			ChannelID: 7, ModelName: "gpt-4o", Mode: manualModePerCall, PriceIn: 0.02,
		}),
	}

	entry, _, priced := buildChannelEntry("gpt-4o", ch, ug, nil, nil, manual, []int{10}, 10, nil)
	assert.True(t, priced)
	assert.Equal(t, "manual", entry.PriceSource)
	assert.Equal(t, "per_call", entry.PriceMode)
	assert.Equal(t, 1.0, entry.UpstreamGroupRatio)

	// A snapshot for the same channel outranks the manual entry.
	snapshots := map[string]map[string]map[string]any{
		channelSourceKey(ch.ChannelName, ch.ChannelID): {
			"gpt-4o": manualPriceFields(&ManualUpstreamPrice{
				ChannelID: 7, ModelName: "gpt-4o", Mode: manualModePerCall, PriceIn: 0.5,
			}),
		},
	}
	entry, _, priced = buildChannelEntry("gpt-4o", ch, ug, nil, snapshots, manual, []int{10}, 10, nil)
	assert.True(t, priced)
	assert.Equal(t, "snapshot", entry.PriceSource)
}
