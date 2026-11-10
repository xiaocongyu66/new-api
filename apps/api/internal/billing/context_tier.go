package billing

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/samber/lo"
)

// BillingModeContextTier selects the simplified "context interval" pricing mode.
// Instead of a free-form expression, the admin stores a list of context tiers
// (intervals keyed by maximum input context length) and each tier carries its
// own per-token prices. The backend converts the stored tiers into the standard
// tiered_expr expression, so all existing billing/settle machinery is reused.
const BillingModeContextTier = "context_tier"

// BillingModeContextTierPerCall is the per-request variant: each context
// interval carries a single fixed USD price charged once per request, instead
// of per-token prices.
const BillingModeContextTierPerCall = "context_tier_per_call"

// ContextTier describes one context interval.
type ContextTier struct {
	Name            string  `json:"name"`
	MaxContext      int64   `json:"max_context"` // 0 means "no upper bound" (catch-all, must be last)
	InputPrice      float64 `json:"input_price"`
	OutputPrice     float64 `json:"output_price"`
	CachePrice      float64 `json:"cache_price,omitempty"`
	CacheWritePrice float64 `json:"cache_write_price,omitempty"`
}

// ContextTierConfig is the persisted shape (model -> JSON).
type ContextTierConfig struct {
	Tiers []ContextTier `json:"tiers"`
}

// ContextCallTier is one interval of the per-request variant: requests whose
// input context length falls in the interval are charged Price USD once.
type ContextCallTier struct {
	Name       string  `json:"name"`
	MaxContext int64   `json:"max_context"` // 0 = catch-all (last)
	Price      float64 `json:"price"`
}

// ContextCallTierConfig is the persisted per-call shape (model -> JSON).
type ContextCallTierConfig struct {
	Tiers []ContextCallTier `json:"tiers"`
}

// IsTieredBillingMode reports whether mode uses the tiered expression engine.
func IsTieredBillingMode(mode string) bool {
	switch mode {
	case BillingModeTieredExpr, BillingModeContextTier, BillingModeContextTierPerCall:
		return true
	}
	return false
}

// GetBillingContextTier returns the stored tier JSON for a model.
func GetBillingContextTier(model string) (string, bool) {
	settings.GlobalConfig.ReadLock()
	defer settings.GlobalConfig.ReadUnlock()
	if tiers, ok := billingSetting.BillingContextTier[model]; ok {
		return tiers, true
	}
	return "", false
}

// GetBillingContextTierCopy returns a copy of the stored tier map.
func GetBillingContextTierCopy() map[string]string {
	settings.GlobalConfig.ReadLock()
	defer settings.GlobalConfig.ReadUnlock()
	return lo.Assign(billingSetting.BillingContextTier)
}

// GetBillingContextTierPerCall returns the stored per-call tier JSON for a model.
func GetBillingContextTierPerCall(model string) (string, bool) {
	settings.GlobalConfig.ReadLock()
	defer settings.GlobalConfig.ReadUnlock()
	tiers, ok := billingSetting.BillingContextTierPerCall[model]
	return tiers, ok
}

// GetBillingContextTierPerCallCopy returns a copy of the stored per-call map.
func GetBillingContextTierPerCallCopy() map[string]string {
	settings.GlobalConfig.ReadLock()
	defer settings.GlobalConfig.ReadUnlock()
	return lo.Assign(billingSetting.BillingContextTierPerCall)
}

// GetBillingExprForModel returns the effective expression for any tiered mode.
func GetBillingExprForModel(model string) (string, bool) {
	mode := GetBillingMode(model)
	if mode == BillingModeTieredExpr {
		expr, ok := GetBillingExpr(model)
		return expr, ok
	}
	if mode == BillingModeContextTier {
		tiers, ok := GetBillingContextTier(model)
		if !ok {
			return "", false
		}
		expr, err := ContextTiersToExpr(tiers)
		if err != nil {
			return "", false
		}
		return expr, true
	}
	if mode == BillingModeContextTierPerCall {
		tiers, ok := GetBillingContextTierPerCall(model)
		if !ok {
			return "", false
		}
		expr, err := ContextCallTiersToExpr(tiers)
		if err != nil {
			return "", false
		}
		return expr, true
	}
	return "", false
}

// validateTierBounds rejects configs that would generate an invalid or
// abusive expression: negative bounds/prices, duplicate bounds, multiple
// catch-alls, or non-finite prices. Callers pass the sorted slice, so equal
// positive bounds are adjacent and a negative bound cannot hide behind the
// sort. Invalid config returns an error, which makes GetBillingExprForModel
// report ok=false and billing fall back to plain ratios instead of charging
// a negative/NaN amount.
func validateTierBounds(tiers []int64, prices [][]float64) error {
	catchAlls := 0
	var prev int64 = -1
	for i, bound := range tiers {
		if bound < 0 {
			return fmt.Errorf("tier %d: max_context must be >= 0", i+1)
		}
		if bound == 0 {
			catchAlls++
			if catchAlls > 1 {
				return fmt.Errorf("only one catch-all tier (max_context 0) is allowed")
			}
		} else {
			if bound <= prev {
				return fmt.Errorf("tier %d: max_context must strictly increase", i+1)
			}
			prev = bound
		}
		for _, price := range prices[i] {
			if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
				return fmt.Errorf("tier %d: prices must be finite and non-negative", i+1)
			}
		}
	}
	return nil
}

// ContextTiersToExpr converts stored context tiers into a tiered_expr expression.
func ContextTiersToExpr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty context tier config")
	}
	var cfg ContextTierConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		// Allow a bare array too.
		var tiers []ContextTier
		if err2 := json.Unmarshal([]byte(raw), &tiers); err2 != nil {
			return "", err
		}
		cfg.Tiers = tiers
	}
	if len(cfg.Tiers) == 0 {
		return "", fmt.Errorf("no context tiers")
	}
	// Sort by MaxContext ascending, catch-all (0) last.
	tiers := append([]ContextTier(nil), cfg.Tiers...)
	sort.SliceStable(tiers, func(i, j int) bool {
		ai, aj := tiers[i].MaxContext, tiers[j].MaxContext
		if ai == 0 {
			ai = 1 << 62
		}
		if aj == 0 {
			aj = 1 << 62
		}
		return ai < aj
	})
	bounds := make([]int64, len(tiers))
	prices := make([][]float64, len(tiers))
	for i, t := range tiers {
		bounds[i] = t.MaxContext
		prices[i] = []float64{t.InputPrice, t.OutputPrice, t.CachePrice, t.CacheWritePrice}
	}
	if err := validateTierBounds(bounds, prices); err != nil {
		return "", err
	}

	var b strings.Builder
	writeContextTernaryChain(&b, tiers, func(t ContextTier) (string, string) {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = "tier"
		}
		// Build price expression terms (only non-zero prices are emitted so the
		// token-param normaliser only subtracts categories we actually charge).
		var terms []string
		if t.InputPrice != 0 {
			terms = append(terms, fmt.Sprintf("p * %g", t.InputPrice))
		}
		if t.OutputPrice != 0 {
			terms = append(terms, fmt.Sprintf("c * %g", t.OutputPrice))
		}
		if t.CachePrice != 0 {
			terms = append(terms, fmt.Sprintf("cr * %g", t.CachePrice))
		}
		if t.CacheWritePrice != 0 {
			terms = append(terms, fmt.Sprintf("cc * %g", t.CacheWritePrice))
		}
		if len(terms) == 0 {
			terms = append(terms, "0")
		}
		return name, strings.Join(terms, " + ")
	}, func(t ContextTier) (int64, bool) {
		return t.MaxContext, t.MaxContext <= 0
	})
	return b.String(), nil
}

// writeContextTernaryChain emits `len <= N ? tier(name, body) : ...` chains.
// boundFor returns the interval's max context and whether it is the catch-all
// (final, unconditioned) tier; the trailing slice element is always treated as
// catch-all. Shared by the per-token and per-call converters.
func writeContextTernaryChain[T any](
	b *strings.Builder,
	tiers []T,
	bodyFor func(t T) (name string, body string),
	boundFor func(t T) (maxContext int64, catchAll bool),
) {
	for i, t := range tiers {
		name, body := bodyFor(t)
		maxContext, catchAll := boundFor(t)
		isLast := i == len(tiers)-1
		// The expression grammar is `cond ? a : b`; the condition must lead.
		// The catch-all/last tier is the final `else` branch, never conditioned.
		if !catchAll && !isLast {
			b.WriteString(fmt.Sprintf("len <= %d ? ", maxContext))
		}
		b.WriteString(fmt.Sprintf("tier(%q, %s)", name, body))
		if catchAll || isLast {
			return
		}
		b.WriteString(" : ")
	}
}

// perCallExprCoefficientCompensation multiplies the stored per-request USD
// price because the expression engine converts raw output as $/1M tokens
// (rawCost / 1_000_000 * QuotaPerUnit). Emitting price*1_000_000 makes the
// engine settle exactly price USD per request.
const perCallExprCoefficientCompensation = 1_000_000

// ContextCallTiersToExpr converts stored per-call context tiers into a
// tiered_expr expression where each tier yields a fixed per-request cost.
func ContextCallTiersToExpr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty context tier config")
	}
	var cfg ContextCallTierConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		// Allow a bare array too.
		var tiers []ContextCallTier
		if err2 := json.Unmarshal([]byte(raw), &tiers); err2 != nil {
			return "", err
		}
		cfg.Tiers = tiers
	}
	if len(cfg.Tiers) == 0 {
		return "", fmt.Errorf("no context tiers")
	}
	tiers := append([]ContextCallTier(nil), cfg.Tiers...)
	sort.SliceStable(tiers, func(i, j int) bool {
		ai, aj := tiers[i].MaxContext, tiers[j].MaxContext
		if ai == 0 {
			ai = 1 << 62
		}
		if aj == 0 {
			aj = 1 << 62
		}
		return ai < aj
	})
	bounds := make([]int64, len(tiers))
	prices := make([][]float64, len(tiers))
	for i, t := range tiers {
		bounds[i] = t.MaxContext
		prices[i] = []float64{t.Price}
	}
	if err := validateTierBounds(bounds, prices); err != nil {
		return "", err
	}

	var b strings.Builder
	writeContextTernaryChain(&b, tiers, func(t ContextCallTier) (string, string) {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = "tier"
		}
		return name, fmt.Sprintf("%g", t.Price*perCallExprCoefficientCompensation)
	}, func(t ContextCallTier) (int64, bool) {
		return t.MaxContext, t.MaxContext <= 0
	})
	return b.String(), nil
}
