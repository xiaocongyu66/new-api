package channel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// defaultRouteStaticWeight is the weight a freshly expanded route unit starts at.
// The legacy channel weight migration keys off it: a row still sitting at this
// value has never been tuned in the route unit UI, so the retiring channel column
// is the only place that channel's intent was recorded.
const defaultRouteStaticWeight = 100

// ChannelModelRoute represents a static weight row for a route unit under a public model alias.
type ChannelModelRoute struct {
	Id               int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Group            string `json:"group" gorm:"type:varchar(64);uniqueIndex:idx_route_unit,priority:1"`
	PublicModelAlias string `json:"public_model_alias" gorm:"type:varchar(255);uniqueIndex:idx_route_unit,priority:2"`
	ChannelId        int    `json:"channel_id" gorm:"uniqueIndex:idx_route_unit,priority:3;index:idx_route_channel"`
	KeyIndex         int    `json:"key_index" gorm:"uniqueIndex:idx_route_unit,priority:4"`
	UpstreamModel    string `json:"upstream_model" gorm:"type:varchar(255);uniqueIndex:idx_route_unit,priority:5"`
	StaticWeight     int    `json:"static_weight"`
	Enabled          bool   `json:"enabled"`
}

// ExpandChannelModelRoutes expands a channel into its route unit rows.
// It is a pure function with no side effects.
func ExpandChannelModelRoutes(channel *Channel) []ChannelModelRoute {
	if channel == nil {
		return nil
	}

	models := channel.GetModels()
	groups := channel.GetGroups()
	if len(models) == 0 || len(groups) == 0 {
		return nil
	}

	// Parse model mapping: origin -> upstream
	var mapping map[string]string
	mappingStr := channel.GetModelMapping()
	if mappingStr != "" {
		if err := common.UnmarshalJsonStr(mappingStr, &mapping); err != nil {
			common.SysError(fmt.Sprintf("failed to unmarshal model_mapping: channel_id=%d, error=%v", channel.Id, err))
			mapping = nil
		}
	}

	// Determine key indices
	var keyIndices []int
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		if len(keys) == 0 {
			return nil
		}
		keyIndices = make([]int, len(keys))
		for i := range keys {
			keyIndices[i] = i
		}
	} else {
		keyIndices = []int{0}
	}

	routes := make([]ChannelModelRoute, 0, len(models)*len(groups)*len(keyIndices))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		upstream := model
		if mapping != nil {
			if v, ok := mapping[model]; ok && v != "" {
				upstream = v
			}
		}
		for _, group := range groups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			for _, keyIndex := range keyIndices {
				routes = append(routes, ChannelModelRoute{
					Group:            group,
					PublicModelAlias: model,
					ChannelId:        channel.Id,
					KeyIndex:         keyIndex,
					UpstreamModel:    upstream,
					StaticWeight:     defaultRouteStaticWeight,
					Enabled:          true,
				})
			}
		}
	}
	return routes
}

// SyncChannelModelRoutesWithTx synchronizes the route rows for a single channel inside a transaction.
// If the channel does not exist, all rows for that channelID are removed and nil is returned.
func SyncChannelModelRoutesWithTx(tx *gorm.DB, channelID int) error {
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}

	var channel Channel
	err := tx.First(&channel, channelID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Where("channel_id = ?", channelID).Delete(&ChannelModelRoute{}).Error
		}
		return err
	}

	expected := ExpandChannelModelRoutes(&channel)
	if len(expected) == 0 {
		return tx.Where("channel_id = ?", channelID).Delete(&ChannelModelRoute{}).Error
	}

	// Fetch existing rows for this channel
	var existing []ChannelModelRoute
	if err := tx.Where("channel_id = ?", channelID).Find(&existing).Error; err != nil {
		return err
	}

	// Build sets keyed by composite unique fields for diff
	type routeUnitKey struct {
		Group            string
		PublicModelAlias string
		ChannelId        int
		KeyIndex         int
		UpstreamModel    string
	}

	existingSet := make(map[routeUnitKey]struct{}, len(existing))
	for _, r := range existing {
		existingSet[routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}] = struct{}{}
	}

	// Build expected set and collect missing
	expectedSet := make(map[routeUnitKey]struct{}, len(expected))
	missing := make([]ChannelModelRoute, 0, len(expected))
	for _, r := range expected {
		key := routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}
		expectedSet[key] = struct{}{}
		if _, ok := existingSet[key]; !ok {
			missing = append(missing, r)
		}
	}

	// Delete stale rows (in existing but not in expected) — batch by primary key
	staleIDs := make([]int, 0, len(existingSet))
	for _, r := range existing {
		key := routeUnitKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}
		if _, ok := expectedSet[key]; !ok {
			staleIDs = append(staleIDs, r.Id)
		}
	}
	if len(staleIDs) > 0 {
		if err := tx.Where("id IN ?", staleIDs).Delete(&ChannelModelRoute{}).Error; err != nil {
			return err
		}
	}

	// Insert missing rows. MutateGatewayRouting serializes all route mutations in a single
	// transaction, so the missing set is computed inside the same tx and conflicts cannot occur.
	if len(missing) > 0 {
		for _, chunk := range lo.Chunk(missing, 50) {
			if err := tx.Create(&chunk).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteChannelModelRoutesByChannelIDsWithTx deletes all route rows for the given channel IDs in a transaction.
func DeleteChannelModelRoutesByChannelIDsWithTx(tx *gorm.DB, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}
	return tx.Where("channel_id IN ?", ids).Delete(&ChannelModelRoute{}).Error
}

// SeedChannelModelRoutes seeds route rows for all existing channels.
// Used at startup to migrate existing channel/ability data into route units.
// Runs atomically inside a transaction so a mid-seed failure rolls back fully
// and the next startup reseeds cleanly.
func SeedChannelModelRoutes() error {
	return dbx.DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := tx.Find(&channels).Error; err != nil {
			return err
		}
		for _, ch := range channels {
			if err := SyncChannelModelRoutesWithTx(tx, ch.Id); err != nil {
				return err
			}
		}
		return nil
	})
}

// RouteUnitView is a flattened view of a route unit joined with channel info.
type RouteUnitView struct {
	Id               int     `json:"id"`
	Group            string  `json:"group"`
	PublicModelAlias string  `json:"public_model_alias"`
	ChannelId        int     `json:"channel_id"`
	ChannelName      string  `json:"channel_name"`
	ChannelStatus    int     `json:"channel_status"`
	BaseURL          string  `json:"base_url"`
	KeyIndex         int     `json:"key_index"`
	UpstreamModel    string  `json:"upstream_model"`
	StaticWeight     int     `json:"static_weight"`
	Enabled          bool    `json:"enabled"`
	ExpectedShare    float64 `json:"expected_share"`
	HealthScore      float64 `json:"health_score"`

	// Runtime EWMA observation, read straight from the in-memory store. These are
	// read-only diagnostics: they make it possible to confirm that the route unit
	// that served a request is the one whose stats moved.
	EwmaQuality float64 `json:"ewma_quality"`
	SuccessEwma float64 `json:"success_ewma"`
	TtftEwmaMs  float64 `json:"ttft_ewma_ms"`
	TpsEwma     float64 `json:"tps_ewma"`
	SampleCount int     `json:"sample_count"`

	// W5.1: six-factor score breakdown for operator verification.
	// final_score ≈ base_weight * ewma_quality * health_multiplier * share_correction
	// expected_share is static-weight ratio (operator's configured intent).
	// RouteScore.ExpectedShare (not exposed here) is base-score share (correction's target).
	BaseWeight         float64 `json:"base_weight"`         // routingBaseWeight(static_weight) as float
	HealthMultiplier   float64 `json:"health_multiplier"`   // state machine multiplier (same as health_score, renamed for clarity)
	ShareCorrection    float64 `json:"share_correction"`    // clamped share-deficit multiplier
	ActualShare        float64 `json:"actual_share"`        // recent measured share (selections/opportunities)
	FinalScore         float64 `json:"final_score"`         // product of all four factors
	ShareOpportunities int     `json:"share_opportunities"` // sample size behind actual_share
	ShareSelections    int     `json:"share_selections"`    // selections within the window
}

// RouteUnitAliasSummary aggregates route units by public model alias.
type RouteUnitAliasSummary struct {
	Alias       string `json:"alias"`
	RouteCount  int    `json:"route_count"`
	TotalWeight int    `json:"total_weight"`
}

// GetRouteUnitViewsByAlias returns all route units for a given public model alias,
// joined with channel information. ExpectedShare is the static-weight ratio
// within the route's own group (F1: selection draws from a single (group, alias)
// pool, so the denominator must be per-group, not across all groups).
func GetRouteUnitViewsByAlias(alias string) ([]RouteUnitView, error) {
	if alias == "" {
		return nil, errors.New("alias is required")
	}
	var routes []ChannelModelRoute
	if err := dbx.DB.Where("public_model_alias = ?", alias).Find(&routes).Error; err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return []RouteUnitView{}, nil
	}
	// Collect channel IDs
	channelIDs := make([]int, 0, len(routes))
	for _, r := range routes {
		channelIDs = append(channelIDs, r.ChannelId)
	}
	// Fetch channel info
	var channels []Channel
	if err := dbx.DB.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelMap := make(map[int]Channel, len(channels))
	for _, ch := range channels {
		channelMap[ch.Id] = ch
	}
	// F1: compute total weight per group. Selection draws from a single
	// (group, alias) pool, so expected_share = static_weight / Σ(weights in the
	// same group). Summing across groups makes a channel in both `default` and
	// `vip` inflate the denominator and halve every route's reported share.
	groupWeights := make(map[string]int)
	for _, r := range routes {
		groupWeights[r.Group] += r.StaticWeight
	}
	// Build views
	views := make([]RouteUnitView, 0, len(routes))
	for _, r := range routes {
		ch, ok := channelMap[r.ChannelId]
		var baseURL string
		if ok && ch.BaseURL != nil {
			baseURL = *ch.BaseURL
		}
		share := 0.0
		if gw := groupWeights[r.Group]; gw > 0 {
			share = float64(r.StaticWeight) / float64(gw)
			// Round to 4 decimal places (preserving existing behaviour)
			share = float64(int64(share*10000+0.5)) / 10000
		}
		view := RouteUnitView{
			Id:               r.Id,
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelId:        r.ChannelId,
			ChannelName:      ch.Name,
			ChannelStatus:    ch.Status,
			BaseURL:          baseURL,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
			StaticWeight:     r.StaticWeight,
			Enabled:          r.Enabled,
			ExpectedShare:    share,
			HealthScore:      RouteWeightMultiplier(RouteKey{ChannelId: r.ChannelId, KeyIndex: r.KeyIndex, Model: r.PublicModelAlias}),
			EwmaQuality:      1.0,
		}
		// GetHandle deliberately does not create state: listing route units must
		// not materialise entries for routes that have never served a request.
		if h := routestats.GetHandle(routestats.RouteKey{
			Group:            r.Group,
			PublicModelAlias: r.PublicModelAlias,
			ChannelID:        r.ChannelId,
			KeyIndex:         r.KeyIndex,
			UpstreamModel:    r.UpstreamModel,
		}); h != nil {
			snap := h.Snapshot()
			view.EwmaQuality = h.Quality().Quality
			view.SuccessEwma = snap.SuccessRate
			view.TtftEwmaMs = snap.TTFTMs
			view.TpsEwma = snap.TPS
			view.SampleCount = snap.SampleCount
		}
		views = append(views, view)
	}
	// W5.1: populate the six-factor score breakdown via scoreCandidates. Views are
	// grouped into their (group, alias) pools first, matching how selection groups
	// them. scoreCandidates calls routestats.Corrections which is a pure read (it
	// takes the pool window's lock but never calls RecordSelection), so this read
	// path cannot move any counter.
	//
	// The per-route lookup must stay scoped to one pool: RouteID carries no group,
	// so a channel serving the same alias in both `default` and `vip` produces two
	// views with identical RouteIDs. A map shared across pools would let the second
	// pool's scores overwrite the first pool's view and report one pool twice.
	poolToViews := make(map[routestats.PoolKey][]*RouteUnitView)
	for i := range views {
		v := &views[i]
		v.BaseWeight = float64(routingBaseWeight(v.StaticWeight))
		v.HealthMultiplier = v.HealthScore
		v.ShareCorrection = 1.0
		v.FinalScore = v.BaseWeight * v.EwmaQuality * v.HealthMultiplier
		pool := routestats.PoolKey{Group: v.Group, PublicModelAlias: v.PublicModelAlias}
		poolToViews[pool] = append(poolToViews[pool], v)
	}
	for pool, poolViews := range poolToViews {
		candidates := make([]routeCandidate, 0, len(poolViews))
		byRouteID := make(map[routestats.RouteID]*RouteUnitView, len(poolViews))
		for _, v := range poolViews {
			candidates = append(candidates, routeCandidate{
				channelId:     v.ChannelId,
				keyIndex:      v.KeyIndex,
				upstreamModel: v.UpstreamModel,
				staticWeight:  v.StaticWeight,
			})
			byRouteID[routestats.RouteID{
				ChannelID:     v.ChannelId,
				KeyIndex:      v.KeyIndex,
				UpstreamModel: v.UpstreamModel,
			}] = v
		}
		scores, _ := scoreCandidates(pool, candidates, alias)
		for id, s := range scores {
			v, ok := byRouteID[id]
			if !ok {
				continue
			}
			v.BaseWeight = s.BaseWeight
			v.EwmaQuality = s.Quality
			v.HealthMultiplier = s.Health
			v.ShareCorrection = s.Correction
			v.ActualShare = s.ActualShare
			v.FinalScore = s.Final
			v.ShareOpportunities = s.Opportunities
			v.ShareSelections = s.Selections
		}
	}
	return views, nil
}

// ListRouteUnitAliases returns all distinct public model aliases with their
// route count and total static weight.
func ListRouteUnitAliases() ([]RouteUnitAliasSummary, error) {
	type agg struct {
		Alias       string
		RouteCount  int
		TotalWeight int
	}
	var results []agg
	if err := dbx.DB.Model(&ChannelModelRoute{}).
		Select("public_model_alias as alias, count(*) as route_count, coalesce(sum(static_weight),0) as total_weight").
		Group("public_model_alias").
		Scan(&results).Error; err != nil {
		return nil, err
	}
	summaries := make([]RouteUnitAliasSummary, 0, len(results))
	for _, r := range results {
		summaries = append(summaries, RouteUnitAliasSummary{
			Alias:       r.Alias,
			RouteCount:  r.RouteCount,
			TotalWeight: r.TotalWeight,
		})
	}
	return summaries, nil
}

// UpdateRouteUnitConfig updates a single route unit by ID.
// weight and enabled are optional (nil = no change). Returns gorm.ErrRecordNotFound if row does not exist.
func UpdateRouteUnitConfig(id int, weight *int, enabled *bool) error {
	if weight == nil && enabled == nil {
		return errors.New("no fields to update")
	}
	updates := make(map[string]any)
	if weight != nil {
		if *weight < 0 {
			return errors.New("static_weight must be >= 0")
		}
		updates["static_weight"] = *weight
	}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	result := dbx.DB.Model(&ChannelModelRoute{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	// Selection reads the in-memory route index, and only InitChannelCache
	// rebuilds it. Without this the endpoint returns 200 while traffic keeps
	// following the old weight — and a route disabled here keeps serving until
	// the next SyncChannelCache tick. Every sibling routing mutation invalidates
	// explicitly for the same reason.
	InitChannelCache()
	return nil
}
