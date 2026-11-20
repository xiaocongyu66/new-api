package ops

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	billing "github.com/QuantumNous/new-api/internal/billing"
	priceexpr "github.com/QuantumNous/new-api/internal/billing/price_expression"
	channel "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	pricingAnalysisDefaultWindow = 131072
	pricingAnalysisLookbackDays  = 7
	// referenceSourceName is the snapshot source used as a reference price when
	// a channel has no snapshot of its own.
	referenceSourceName = "models.dev"
	// SQLite caps bind variables at 999 in default builds; 500 leaves room for
	// the other placeholders in the same statements.
	pricingAnalysisQueryChunkSize = 500
)

var pricingAnalysisDefaultWindows = []int{8192, 32768, 131072, 200000, 1000000}

// PricingAnalysis handles POST /api/ratio_sync/analysis.
//
// It compares each plaza alias's local billing price against the upstream price
// persisted by /fetch (each source's latest snapshot batch), evaluated over the
// context windows, and attaches usage/revenue/cost/profit aggregated from the
// usage table over the requested time range.
func PricingAnalysis(c contract.Context) {
	var req dto.PricingAnalysisRequest
	if err := c.BindJSON(&req); err != nil {
		common.SysError("failed to bind pricing analysis request: " + err.Error())
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "请求参数格式错误"})
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = defaultTimeoutSeconds
	}

	now := common.GetTimestamp()
	end := req.End
	if end == 0 {
		end = now
	}
	start := req.Start
	if start == 0 {
		start = end - pricingAnalysisLookbackDays*86400
	}

	windows := req.Windows
	if len(windows) == 0 {
		windows = make([]int, len(pricingAnalysisDefaultWindows))
		copy(windows, pricingAnalysisDefaultWindows)
	}
	canonicalWindow := closestWindow(windows, pricingAnalysisDefaultWindow)

	vendor := ""
	if req.Vendor != 0 {
		vendor = strconv.Itoa(req.Vendor)
	}
	models, _, err := channel.SearchModels("", vendor, "", "", 0, 5000)
	if err != nil {
		common.SysError("failed to search models: " + err.Error())
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": "查询模型失败"})
		return
	}
	enrichModels(models)

	vendorRows, _ := channel.GetAllVendors(0, 5000)
	vendorName := make(map[int]string, len(vendorRows))
	for _, v := range vendorRows {
		vendorName[v.Id] = v.Name
	}

	// Local pricing fields, keyed by model name then field name.
	localData := getLocalPricingSyncData()
	localFields := make(map[string]map[string]any)
	for field, byModel := range localData {
		modelMap := valueMap(byModel)
		for modelName, value := range modelMap {
			if localFields[modelName] == nil {
				localFields[modelName] = make(map[string]any)
			}
			localFields[modelName][field] = value
		}
	}
	// Upstream prices are keyed by source (channel) then model. Channels that
	// have no snapshot of their own fall back to a reference price source
	// (e.g. the models.dev preset) so the matrix still shows comparable costs.
	sources := upstreamSources(req.Upstreams)
	snapshotBySource := map[string]map[string]map[string]any{}
	referenceFields := map[string]map[string]any{}
	if batch, bySource := latestSnapshotValues(sources); batch > 0 {
		snapshotBySource = bySource
	}
	// The models.dev preset is a shared reference price source, not a channel.
	// Its snapshot key carries a suffix like "models.dev 价格预设(-101)", so
	// match by prefix. No arbitrary channel price should be used as reference.
	for source, byModel := range snapshotBySource {
		if strings.HasPrefix(source, referenceSourceName) {
			referenceFields = byModel
			break
		}
	}

	matchedSets := make([][]string, len(models))
	allMatched := make([]string, 0, len(models))
	seenMatched := make(map[string]struct{}, len(models))
	for i, mm := range models {
		matched := mm.MatchedModels
		if len(matched) == 0 {
			// Exact rule: the alias itself is the matched model name.
			matched = []string{mm.ModelName}
		}
		matchedSets[i] = matched
		for _, m := range matched {
			if _, ok := seenMatched[m]; !ok {
				seenMatched[m] = struct{}{}
				allMatched = append(allMatched, m)
			}
		}
	}

	// Batch every model aggregation exactly once instead of once per alias
	// (was ~3 queries × up to 5000 aliases). Per-alias data is indexed back
	// out of the result maps below, keeping identical numbers.
	bchans, err := boundChannelsForModels(allMatched)
	if err != nil {
		common.SysError("failed to query bound channels: " + err.Error())
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": "查询绑定渠道失败"})
		return
	}
	usageMap, err := usageAggregates(allMatched, start, end)
	if err != nil {
		common.SysError("failed to aggregate usage: " + err.Error())
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": "聚合使用量失败"})
		return
	}
	tierUsageMap := tierUsageByWindow(allMatched, windows, start, end)

	rows := make([]dto.AnalysisRow, 0, len(models))
	for i, mm := range models {
		matched := matchedSets[i]
		totalChannels := 0
		for _, m := range matched {
			totalChannels += len(bchans[m])
		}
		if totalChannels == 0 {
			continue
		}

		row := dto.AnalysisRow{
			Alias:        mm.ModelName,
			VendorID:     mm.VendorID,
			VendorName:   vendorName[mm.VendorID],
			NameRule:     mm.NameRule,
			MatchedCount: len(matched),
		}

		// Wildcard aliases have no price of their own; the busiest matched model
		// drives the representative price shown in the UI.
		priceModel := ""
		priceTokens := int64(-1)
		var requests, tokens, quota int64
		cost := 0.0
		// Row-level cost/profit/margin are only set when every bound channel had
		// an upstream price; a partial upstream cost overstates profit.
		pricedChannels := 0
		for _, m := range matched {
			chs := bchans[m]
			modelTokens := int64(0)
			chEntries := make([]dto.AnalysisChannel, 0, len(chs))
			for _, ch := range chs {
				ug := usageMap[[2]any{int64(ch.ChannelID), m}]
				revenue := float64(ug.Quota) / float64(common.QuotaPerUnit)
				// Upstream price belongs to the channel, not the model: look it
				// up under this channel's snapshot source key. Aliases that
				// differ from the upstream model name are resolved conservatively.
				sourceKey := channelSourceKey(ch.ChannelName, ch.ChannelID)
				upFields := resolveUpstreamFields(m, snapshotBySource[sourceKey], referenceFields, ch.ModelMapping)
				upstreamExpr, _ := modelExpr(upFields)
				entry := dto.AnalysisChannel{
					ChannelID:   ch.ChannelID,
					ChannelName: ch.ChannelName,
					ChannelType: ch.ChannelType,
					ModelName:   m,
					PriceMode:   priceModeFromFields(upstreamExpr, upFields),
					BillingMode: billingModeFromFields(upFields),
					Requests:    ug.Requests,
					Tokens:      ug.Tokens,
					Revenue:     revenue,
					Windows:     channelWindows(localFields[m], upFields, windows, ch.ChannelID, m, tierUsageMap),
				}
				if upstreamExpr != "" {
					if cwUsd := evalUsd(upstreamExpr, canonicalWindow); cwUsd != nil && canonicalWindow > 0 {
						var chCost float64
						if strings.Contains(upstreamExpr, "fixed(") {
							chCost = *cwUsd * float64(ug.Requests)
						} else {
							unit := *cwUsd / float64(canonicalWindow+canonicalWindow/4)
							chCost = unit * float64(ug.Tokens)
						}
						entry.Cost = &chCost
						profit := revenue - chCost
						entry.Profit = &profit
						if revenue > 0 {
							margin := profit / revenue * 100
							entry.MarginPct = &margin
						}
						cost += chCost
						pricedChannels++
					}
				}
				chEntries = append(chEntries, entry)
				requests += ug.Requests
				tokens += ug.Tokens
				quota += ug.Quota
				modelTokens += ug.Tokens
			}
			if modelTokens > priceTokens {
				priceTokens = modelTokens
				priceModel = m
			}
			row.Channels = append(row.Channels, chEntries...)
		}

		row.PriceModel = priceModel
		row.Requests = requests
		row.Tokens = tokens
		row.Revenue = float64(quota) / float64(common.QuotaPerUnit)

		if pricedChannels == totalChannels {
			row.Cost = &cost
			profit := row.Revenue - cost
			row.Profit = &profit
			if row.Revenue > 0 {
				margin := profit / row.Revenue * 100
				row.MarginPct = &margin
			}
		}

		rows = append(rows, row)
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": dto.PricingAnalysisData{
		Windows: windows,
		Vendors: allVendorsDTO(vendorRows),
		Rows:    rows,
	}})
}

// channelWindows returns local vs upstream price at each context window for a
// bound channel's model. tier holds individual consume-log samples keyed by
// (channel/model/window); their real token counts drive per-window cost.
func channelWindows(localFields, upstreamFields map[string]any, windows []int, channelID int, modelName string, tier map[[3]any][]pricingSample) []dto.AnalysisChannelWindow {
	localExpr, _ := modelExpr(localFields)
	upstreamExpr, _ := modelExpr(upstreamFields)
	out := make([]dto.AnalysisChannelWindow, 0, len(windows))
	for _, w := range windows {
		entry := dto.AnalysisChannelWindow{Context: w}
		if localUsd := evalUsd(localExpr, w); localUsd != nil {
			entry.Local = localUsd
		}
		if upstreamUsd := evalUsd(upstreamExpr, w); upstreamUsd != nil {
			entry.Upstream = upstreamUsd
		}
		if entry.Local != nil && entry.Upstream != nil && *entry.Local > 0 {
			diff := (*entry.Upstream - *entry.Local) / *entry.Local * 100
			entry.DiffPct = &diff
			margin := (*entry.Local - *entry.Upstream) / *entry.Local * 100
			entry.MarginPct = &margin
		}
		samples := tier[[3]any{int64(channelID), modelName, w}]
		if len(samples) > 0 {
			requests := int64(len(samples))
			entry.Requests = &requests
			cost := windowCostUSD(upstreamExpr, samples)
			entry.Cost = &cost
			var quota int64
			for _, s := range samples {
				quota += s.Quota
			}
			revenue := float64(quota) / float64(common.QuotaPerUnit)
			profit := revenue - cost
			entry.Profit = &profit
		}
		out = append(out, entry)
	}
	return out
}

// windowCostUSD prices each consume log at its own token counts. Per-token
// expressions scale by p and c inside the expression (no 1.25x context
// divisor); fixed(...) returns its per-request USD. One evaluation per sample
// therefore covers both families exactly.
func windowCostUSD(expr string, samples []pricingSample) float64 {
	if expr == "" {
		return 0
	}
	var total float64
	for _, s := range samples {
		if usd := evalUsdWithTokens(expr, s.Prompt, s.Completion); usd != nil {
			total += *usd
		}
	}
	return total
}

// channelSourceKey rebuilds the snapshot source key for a bound channel. It
// must match the uniqueName format used when persisting snapshots.
func channelSourceKey(name string, id int) string {
	if id != 0 {
		return fmt.Sprintf("%s(%d)", name, id)
	}
	return name
}

// upstreamAliasCandidates returns the ordered fallback names for a plaza alias
// whose upstream snapshot name may differ from the local name. Exact names win;
// only misses strip known prefixes ([套餐], a/) and consult model_mapping.
func upstreamAliasCandidates(modelName string, mapping map[string]string) []string {
	candidates := []string{modelName}
	if name := strings.TrimSpace(strings.TrimPrefix(modelName, "[套餐]")); name != "" && name != modelName {
		candidates = append(candidates, name)
	}
	if name := strings.TrimSpace(strings.TrimPrefix(modelName, "a/")); name != "" && name != modelName {
		candidates = append(candidates, name)
	}
	if mapped := mapping[modelName]; mapped != "" {
		candidates = append(candidates, mapped)
	}
	return candidates
}

// resolveUpstreamFields maps a plaza alias to its upstream price fields.
// Candidates are tried in order against the channel's snapshot model map, then
// against the shared reference map; only the first candidate with non-empty
// fields wins, so exact lookups keep working and only misses use fallbacks.
func resolveUpstreamFields(modelName string, sourceModels, referenceModels map[string]map[string]any, mapping map[string]string) map[string]any {
	candidates := upstreamAliasCandidates(modelName, mapping)
	for _, models := range []map[string]map[string]any{sourceModels, referenceModels} {
		for _, name := range candidates {
			if fields := models[name]; len(fields) > 0 {
				return fields
			}
		}
	}
	return nil
}

// hasPricingFields reports whether a field map carries any billing
// configuration. Missing upstream snapshots must not be labelled per-token.
func hasPricingFields(fields map[string]any) bool {
	if len(fields) == 0 {
		return false
	}
	if mode, _ := fields[billing.BillingModeField].(string); mode != "" {
		return true
	}
	if _, ok := asFloat64(fields["model_price"]); ok {
		return true
	}
	if _, ok := asFloat64(fields["model_ratio"]); ok {
		return true
	}
	if expr, _ := fields[billing.BillingExprField].(string); strings.TrimSpace(expr) != "" {
		return true
	}
	if tier, _ := fields["billing_context_tier"].(string); strings.TrimSpace(tier) != "" {
		return true
	}
	if tier, _ := fields["billing_context_tier_per_call"].(string); strings.TrimSpace(tier) != "" {
		return true
	}
	return false
}

// billingModeFromFields returns the stored billing mode verbatim so the UI can
// distinguish all five editor modes (per-token / per-request / tiered_expr /
// context_tier / context_tier_per_call) instead of the two collapsed families.
// It returns "" when no billing configuration is available.
func billingModeFromFields(fields map[string]any) string {
	if !hasPricingFields(fields) {
		return ""
	}
	mode, _ := fields[billing.BillingModeField].(string)
	if mode != "" {
		return mode
	}
	if _, ok := asFloat64(fields["model_price"]); ok {
		if _, hasRatio := asFloat64(fields["model_ratio"]); !hasRatio {
			return "per-request"
		}
	}
	return "per-token"
}

// priceModeFromFields classifies the billing method the same way the relay
// charges it: a tiered-family mode wins, otherwise a stored fixed ModelPrice
// bills per request and a stored model ratio bills per token.
// It returns "" when no billing configuration is available.
func priceModeFromFields(expr string, fields map[string]any) string {
	if !hasPricingFields(fields) {
		return ""
	}
	mode, _ := fields[billing.BillingModeField].(string)
	switch mode {
	case billing.BillingModeTieredExpr:
		if strings.Contains(expr, "fixed(") {
			return "per_call_tiered"
		}
		return "per_token_tiered"
	case billing.BillingModeContextTier:
		return "per_token_tiered"
	case billing.BillingModeContextTierPerCall:
		return "per_call_tiered"
	}
	if _, ok := asFloat64(fields["model_price"]); ok {
		return "per_call"
	}
	if _, ok := asFloat64(fields["model_ratio"]); ok {
		return "per_token"
	}
	if expr == "" {
		return ""
	}
	if strings.Contains(expr, "fixed(") {
		return "per_call"
	}
	return "per_token"
}

// allVendorsDTO converts catalog vendors to DTO vendors.
func allVendorsDTO(vendors []*channel.Vendor) []dto.AnalysisVendor {
	out := make([]dto.AnalysisVendor, 0, len(vendors))
	for _, v := range vendors {
		out = append(out, dto.AnalysisVendor{ID: v.Id, Name: v.Name})
	}
	return out
}

// modelExpr builds an executable price expression for a model from its field
// map. Tiered modes use their expression (or converted context tiers); plain
// ratio mode is rendered as p * inputPrice + c * outputPrice with $/1M
// coefficients. Returns ok=false when the model has no price configuration.
func modelExpr(fields map[string]any) (string, bool) {
	if len(fields) == 0 {
		return "", false
	}
	mode, _ := fields[billing.BillingModeField].(string)
	expr, _ := fields[billing.BillingExprField].(string)
	ctxTier, _ := fields["billing_context_tier"].(string)
	ctxTierPerCall, _ := fields["billing_context_tier_per_call"].(string)
	if e, ok := tieredExpr(mode, expr, ctxTier, ctxTierPerCall); ok {
		return e, true
	}
	// Mirrors ModelPriceHelper: a tiered-family mode wins; otherwise a stored
	// fixed ModelPrice bills per request and a stored ratio bills per token.
	// A stale ModelPrice on a ratio-billed model must not win.
	if mp, ok := asFloat64(fields["model_price"]); ok {
		if _, hasRatio := asFloat64(fields["model_ratio"]); !hasRatio {
			return fmt.Sprintf("tier(\"fixed\", fixed(%g))", mp), true
		}
	}
	mR, hasRatio := asFloat64(fields["model_ratio"])
	if !hasRatio {
		return "", false
	}
	cR, _ := asFloat64(fields["completion_ratio"])
	factor := 1_000_000.0 / common.QuotaPerUnit
	inPrice := mR * factor
	outPrice := mR * cR * factor
	return fmt.Sprintf("p * %g + c * %g", inPrice, outPrice), true
}

// tieredExpr normalizes a tiered billing mode into one executable expression.
// Dispatch mirrors billing.GetBillingExprForModel: the mode decides which
// stored payload is authoritative, so a stale expression on a context-tier
// model cannot shadow its tiers.
func tieredExpr(mode, expr, ctxTier, ctxTierPerCall string) (string, bool) {
	switch mode {
	case billing.BillingModeTieredExpr:
		if strings.TrimSpace(expr) == "" {
			return "", false
		}
		return expr, true
	case billing.BillingModeContextTier:
		if strings.TrimSpace(ctxTier) == "" {
			return "", false
		}
		if e, err := billing.ContextTiersToExpr(ctxTier); err == nil {
			return e, true
		}
		return "", false
	case billing.BillingModeContextTierPerCall:
		if strings.TrimSpace(ctxTierPerCall) == "" {
			return "", false
		}
		if e, err := billing.ContextCallTiersToExpr(ctxTierPerCall); err == nil {
			return e, true
		}
		return "", false
	}
	// No mode entry: fall back to a stored expression, like GetBillingMode.
	if strings.TrimSpace(expr) != "" {
		return expr, true
	}
	return "", false
}

// evalUsd evaluates an expression at a context window and returns USD
// (raw output is $/1M tokens, so divide by 1e6). Nil on missing/failed eval.
func evalUsd(expr string, ctx int) *float64 {
	return evalUsdWithTokens(expr, ctx, ctx/4)
}

// evalUsdWithTokens evaluates an expression with exact per-request token counts.
// len is set to prompt tokens so tier conditions resolve against the real
// context, exactly like settlement (consume_quota_realtime.go).
func evalUsdWithTokens(expr string, prompt, completion int) *float64 {
	if expr == "" {
		return nil
	}
	cost, _, err := priceexpr.RunExpr(expr, priceexpr.TokenParams{
		P:   float64(prompt),
		C:   float64(completion),
		Len: float64(prompt),
	})
	if err != nil {
		return nil
	}
	usd := cost / 1e6
	return &usd
}

// closestWindow returns the window value closest to target.
func closestWindow(windows []int, target int) int {
	if len(windows) == 0 {
		return pricingAnalysisDefaultWindow
	}
	best := windows[0]
	bestDiff := absInt(best - target)
	for _, w := range windows[1:] {
		if d := absInt(w - target); d < bestDiff {
			bestDiff = d
			best = w
		}
	}
	return best
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// upstreamSources reconstructs the unique source names used when persisting
// snapshots (name, or name(id) when an id is present).
func upstreamSources(upstreams []dto.UpstreamDTO) []string {
	sources := make([]string, 0, len(upstreams))
	for _, u := range upstreams {
		if u.ID != 0 {
			sources = append(sources, fmt.Sprintf("%s(%d)", u.Name, u.ID))
		} else if u.Name != "" {
			sources = append(sources, u.Name)
		}
	}
	return sources
}

// latestSnapshotValues returns each source's most recent fetched_at batch with
// prices nested by source then model then field. Channels fetched in different
// batches must all stay visible, so the latest batch is chosen per source; the
// returned batch is the maximum across sources (only used as a >0 signal).
func latestSnapshotValues(sources []string) (int64, map[string]map[string]map[string]any) {
	type sourceBatch struct {
		Source string
		Batch  int64
	}
	q := dbx.DB.Model(&UpstreamPriceSnapshot{}).
		Select("source, MAX(fetched_at) AS batch")
	if len(sources) > 0 {
		q = q.Where("source IN ?", sources)
	}
	var batches []sourceBatch
	if err := q.Group("source").Scan(&batches).Error; err != nil {
		return 0, nil
	}
	if len(batches) == 0 {
		return 0, nil
	}
	sort.Slice(batches, func(i, j int) bool { return batches[i].Source < batches[j].Source })
	bySource := make(map[string]map[string]map[string]any)
	maxBatch := batches[0].Batch
	for _, sb := range batches {
		if sb.Batch > maxBatch {
			maxBatch = sb.Batch
		}
		var rows []UpstreamPriceSnapshot
		if err := dbx.DB.Model(&UpstreamPriceSnapshot{}).
			Where("source = ? AND fetched_at = ?", sb.Source, sb.Batch).
			Order("model_name, field").
			Find(&rows).Error; err != nil {
			return 0, nil
		}
		for _, r := range rows {
			byModel := bySource[r.Source]
			if byModel == nil {
				byModel = make(map[string]map[string]any)
				bySource[r.Source] = byModel
			}
			fields := byModel[r.ModelName]
			if fields == nil {
				fields = make(map[string]any)
				byModel[r.ModelName] = fields
			}
			if _, ok := fields[r.Field]; ok {
				continue
			}
			var value any
			if err := common.Unmarshal([]byte(r.Value), &value); err == nil {
				fields[r.Field] = value
			}
		}
	}
	return maxBatch, bySource
}

// PriceFluctuation handles GET /api/ratio_sync/fluctuation.
//
// It compares the two most recent distinct fetch batches in the snapshot table
// and reports fields whose value changed by at least threshold_pct, plus newly
// added or removed fields.
func PriceFluctuation(c contract.Context) {
	thresholdPct := 10.0
	if s := c.DefaultQuery("threshold_pct", ""); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			thresholdPct = f
		}
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": dto.FluctuationData{Changes: computeFluctuation(thresholdPct)}})
}

// computeFluctuation compares the two most recent distinct snapshot batches.
// It returns at most 200 changes sorted by source, model, field.
func computeFluctuation(thresholdPct float64) []dto.FluctuationChange {
	var batches []int64
	if err := dbx.DB.Model(&UpstreamPriceSnapshot{}).
		Distinct().Order("fetched_at DESC").Limit(2).Pluck("fetched_at", &batches).Error; err != nil {
		common.SysError("failed to query snapshot batches: " + err.Error())
		return []dto.FluctuationChange{}
	}
	if len(batches) < 2 {
		return []dto.FluctuationChange{}
	}
	newer, older := batches[0], batches[1]

	var rows []UpstreamPriceSnapshot
	if err := dbx.DB.Model(&UpstreamPriceSnapshot{}).
		Where("fetched_at IN ?", batches).Find(&rows).Error; err != nil {
		common.SysError("failed to query snapshot rows: " + err.Error())
		return []dto.FluctuationChange{}
	}

	type snapKey struct {
		source, model, field string
	}
	newVals := make(map[snapKey]string)
	oldVals := make(map[snapKey]string)
	for _, r := range rows {
		k := snapKey{r.Source, r.ModelName, r.Field}
		if r.FetchedAt == newer {
			newVals[k] = r.Value
		} else if r.FetchedAt == older {
			oldVals[k] = r.Value
		}
	}

	keys := make(map[snapKey]struct{})
	for k := range newVals {
		keys[k] = struct{}{}
	}
	for k := range oldVals {
		keys[k] = struct{}{}
	}

	// Each /fetch persists only the channels selected for that fetch, so the
	// two batches generally cover different channel scopes. Restrict the
	// comparison to sources present in BOTH batches: a source missing from the
	// newer batch was simply not fetched, not a price removal.
	newSources := make(map[string]struct{})
	for k := range newVals {
		newSources[k.source] = struct{}{}
	}
	bothSources := make(map[string]struct{})
	for k := range oldVals {
		if _, ok := newSources[k.source]; ok {
			bothSources[k.source] = struct{}{}
		}
	}

	type change struct {
		source    string
		model     string
		field     string
		oldValue  any
		newValue  any
		changePct *float64
	}
	var changes []change
	for k := range keys {
		if _, ok := bothSources[k.source]; !ok {
			continue
		}
		nv, hasNew := newVals[k]
		ov, hasOld := oldVals[k]
		var cp *float64
		include := false
		if hasNew && hasOld {
			nf, nerr := snapshotValueToFloat(nv)
			of, oerr := snapshotValueToFloat(ov)
			if nerr == nil && oerr == nil {
				if of != 0 {
					pct := (nf - of) / of * 100
					if math.Abs(pct) >= thresholdPct {
						cp = &pct
						include = true
					}
				} else if nf != of {
					include = true
				}
			} else if nv != ov {
				// Non-numeric fields (billing mode / expression) still matter.
				include = true
			}
		} else {
			include = true
		}
		if !include {
			continue
		}
		var oldV, newV any
		if hasOld {
			oldV = snapshotValue(ov)
		}
		if hasNew {
			newV = snapshotValue(nv)
		}
		changes = append(changes, change{k.source, k.model, k.field, oldV, newV, cp})
	}

	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].source != changes[j].source {
			return changes[i].source < changes[j].source
		}
		if changes[i].model != changes[j].model {
			return changes[i].model < changes[j].model
		}
		return changes[i].field < changes[j].field
	})
	if len(changes) > 200 {
		changes = changes[:200]
	}

	out := make([]dto.FluctuationChange, 0, len(changes))
	for _, ch := range changes {
		out = append(out, dto.FluctuationChange{
			Source:    ch.source,
			Model:     ch.model,
			Field:     ch.field,
			OldValue:  ch.oldValue,
			NewValue:  ch.newValue,
			ChangePct: ch.changePct,
			FetchedAt: newer,
		})
	}
	return out
}

func snapshotValueToFloat(v string) (float64, error) {
	var f float64
	if err := common.Unmarshal([]byte(v), &f); err != nil {
		return 0, err
	}
	return f, nil
}

func snapshotValue(v string) any {
	var x any
	if err := common.Unmarshal([]byte(v), &x); err != nil {
		return v
	}
	return x
}

// boundChannelRow is a channel binding for a model, including the channel id.
// ModelMapping holds this channel's alias→upstream model map, used to resolve
// plaza aliases whose snapshot names differ from the local alias.
type boundChannelRow struct {
	ChannelID   int
	ChannelName string
	ChannelType int
	ModelName   string
	// ModelMapping may be nil when the channel has no mapping configured.
	ModelMapping map[string]string
}

// boundChannelsForModels queries abilities+channels for the given model names.
// Model names are queried in chunks so IN clauses stay below SQLite's variable
// limit when thousands of matched models are analyzed at once.
func boundChannelsForModels(modelNames []string) (map[string][]boundChannelRow, error) {
	out := make(map[string][]boundChannelRow)
	if len(modelNames) == 0 {
		return out, nil
	}
	type row struct {
		ChannelID       int
		ChannelName     string
		ChannelType     int
		ModelName       string
		ModelMappingRaw *string `gorm:"column:model_mapping"`
	}
	for _, chunk := range chunkStrings(modelNames, pricingAnalysisQueryChunkSize) {
		var rows []row
		if err := dbx.DB.Table("abilities a").
			Select("a.channel_id as channel_id, c.name as channel_name, c.type as channel_type, a.model as model_name, c.model_mapping as model_mapping").
			Joins("JOIN channels c ON c.id = a.channel_id").
			Where("a.model IN ? AND a.enabled = ?", chunk, true).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			var mapping map[string]string
			if r.ModelMappingRaw != nil && *r.ModelMappingRaw != "" && *r.ModelMappingRaw != "{}" {
				mapping = map[string]string{}
				if err := common.Unmarshal([]byte(*r.ModelMappingRaw), &mapping); err != nil {
					common.SysError(fmt.Sprintf("failed to unmarshal model_mapping for channel %d: %v", r.ChannelID, err))
				}
			}
			out[r.ModelName] = append(out[r.ModelName], boundChannelRow{
				ChannelID:    r.ChannelID,
				ChannelName:  r.ChannelName,
				ChannelType:  r.ChannelType,
				ModelName:    r.ModelName,
				ModelMapping: mapping,
			})
		}
	}
	return out, nil
}

// chunkStrings splits items into chunks of size so IN clauses stay below
// SQLite's default MAX_VARIABLE_NUMBER (999). The last chunk may be shorter.
func chunkStrings(items []string, size int) [][]string {
	if size <= 0 || len(items) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}

// pricingSample is one consume log, retained with the exact token counts that
// produced its charge. Tier is the matched_tier recorded in the log's `other`
// JSON; the expression itself reselects the tier via len = Prompt.
type pricingSample struct {
	Prompt     int
	Completion int
	Quota      int64
	Tier       string
}

// tierUsageByWindow buckets raw consume logs into the requested prompt-token
// windows. Keeping individual logs (rather than averaging them) lets the
// analysis price each request at its real token counts, so tier boundaries
// that do not align with the display windows no longer skew the cost.
func tierUsageByWindow(modelNames []string, windows []int, start, end int64) map[[3]any][]pricingSample {
	out := make(map[[3]any][]pricingSample)
	if dbx.LogDB == nil || len(modelNames) == 0 || len(windows) == 0 {
		return out
	}
	type row struct {
		ChannelID        int64
		ModelName        string
		PromptTokens     int
		CompletionTokens int
		Quota            int64
		Other            string
	}
	for _, chunk := range chunkStrings(modelNames, pricingAnalysisQueryChunkSize) {
		var rows []row
		if err := dbx.LogDB.Table("logs").
			Select("channel_id, model_name, prompt_tokens, completion_tokens, quota, other").
			Where("model_name IN ? AND created_at BETWEEN ? AND ?", chunk, start, end).
			Where("type = ?", usage.LogTypeConsume).
			Scan(&rows).Error; err != nil {
			common.SysError("failed to load tier usage logs: " + err.Error())
			continue
		}
		for _, r := range rows {
			var meta map[string]any
			tier := ""
			if r.Other != "" && common.Unmarshal([]byte(r.Other), &meta) == nil {
				tier, _ = meta["matched_tier"].(string)
			}
			key := [3]any{r.ChannelID, r.ModelName, windows[sampleBucket(r.PromptTokens, windows)]}
			out[key] = append(out[key], pricingSample{
				Prompt:     r.PromptTokens,
				Completion: r.CompletionTokens,
				Quota:      r.Quota,
				Tier:       tier,
			})
		}
	}
	return out
}

// sampleBucket mirrors the old SQL CASE ordering: the first window whose
// upper bound is >= prompt_tokens wins, with the last window as catch-all.
func sampleBucket(promptTokens int, windows []int) int {
	for i, w := range windows {
		if promptTokens <= w {
			return i
		}
	}
	return len(windows) - 1
}

// usageAgg is a per (channel_id, model_name) usage aggregation.
type usageAgg struct {
	ChannelID int64
	ModelName string
	Tokens    int64
	Requests  int64
	Quota     int64
}

// usageAggregates aggregates quota_data for the given models in [start, end].
// Models are queried in chunks so IN clauses stay below SQLite's variable limit.
func usageAggregates(modelNames []string, start, end int64) (map[[2]any]usageAgg, error) {
	out := make(map[[2]any]usageAgg)
	if len(modelNames) == 0 {
		return out, nil
	}
	type row struct {
		ChannelID int64
		ModelName string
		Tokens    int64
		Requests  int64
		Quota     int64
	}
	for _, chunk := range chunkStrings(modelNames, pricingAnalysisQueryChunkSize) {
		var rows []row
		if err := dbx.DB.Model(&usage.QuotaData{}).
			Select("channel_id, model_name, sum(token_used) as tokens, sum(count) as requests, sum(quota) as quota").
			Where("model_name IN ? AND created_at BETWEEN ? AND ?", chunk, start, end).
			Group("channel_id, model_name").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[[2]any{r.ChannelID, r.ModelName}] = usageAgg{
				ChannelID: r.ChannelID,
				ModelName: r.ModelName,
				Tokens:    r.Tokens,
				Requests:  r.Requests,
				Quota:     r.Quota,
			}
		}
	}
	return out, nil
}
