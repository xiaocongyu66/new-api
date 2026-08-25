package model

import (
	"errors"
	"math"
	"math/rand/v2"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/routestats"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type SelectedRoute struct {
	RouteId       int // channel_model_routes.id
	Group         string
	Alias         string   // public model alias (== requested model name)
	Channel       *Channel // full channel object
	ChannelId     int
	KeyIndex      int
	Key           string                  // selected API key plaintext (multi-key: by key_index; single key: channel.Key)
	UpstreamModel string                  // mapped upstream model name
	StatsHandle   *routestats.RouteHandle // EWMA stats handle for this route unit
}

// routeCandidate is a normalized candidate for selection.
// Both memory-cache and DB paths produce the same slice of these.
type routeCandidate struct {
	routeId       int
	channelId     int
	keyIndex      int
	upstreamModel string
	staticWeight  int
}

// group2alias2routes maps group -> alias -> route units (enabled only).
// Built and refreshed under channelSyncLock alongside channelsIDM.
var group2alias2routes map[string]map[string][]routeCandidate
var group2alias2routesMu sync.RWMutex // protects group2alias2routes

// buildGroupAliasRoutesFromDB loads enabled route units from channel_model_routes table
// and constructs the group->alias->[]routeCandidate index.
// Caller MUST hold channelSyncLock (write).
func buildGroupAliasRoutesFromDB() {
	group2alias2routes = make(map[string]map[string][]routeCandidate)
	var routes []ChannelModelRoute
	if err := DB.Where("enabled = ?", true).Find(&routes).Error; err != nil {
		common.SysError("failed to load channel_model_routes: " + err.Error())
		return
	}
	for _, r := range routes {
		if _, ok := group2alias2routes[r.Group]; !ok {
			group2alias2routes[r.Group] = make(map[string][]routeCandidate)
		}
		group2alias2routes[r.Group][r.PublicModelAlias] = append(
			group2alias2routes[r.Group][r.PublicModelAlias],
			routeCandidate{
				routeId:       r.Id,
				channelId:     r.ChannelId,
				keyIndex:      r.KeyIndex,
				upstreamModel: r.UpstreamModel,
				staticWeight:  r.StaticWeight,
			},
		)
	}
}

// getCandidatesFromCache returns candidates for the given group and alias from memory index.
// Caller MUST hold channelSyncLock (read).
func getCandidatesFromCache(group, alias string) []routeCandidate {
	if group2alias2routes == nil {
		return nil
	}
	if aliasRoutes, ok := group2alias2routes[group]; ok {
		if candidates, ok := aliasRoutes[alias]; ok {
			return candidates
		}
	}
	return nil
}

// getCandidatesFromDB returns candidates for the given group and alias from database.
func getCandidatesFromDB(group, alias string) []routeCandidate {
	var routes []ChannelModelRoute
	if err := DB.Where("\"group\" = ? AND public_model_alias = ? AND enabled = ?", group, alias, true).Find(&routes).Error; err != nil {
		return nil
	}
	candidates := make([]routeCandidate, 0, len(routes))
	for _, r := range routes {
		candidates = append(candidates, routeCandidate{
			routeId:       r.Id,
			channelId:     r.ChannelId,
			keyIndex:      r.KeyIndex,
			upstreamModel: r.UpstreamModel,
			staticWeight:  r.StaticWeight,
		})
	}
	return candidates
}

// filterCandidatesByChannelStatusAndKey filters candidates by channel status (enabled),
// key availability (multi-key status), and Advanced Custom path filtering.
// Modifies the slice in place and returns the filtered result.
func filterCandidatesByChannelStatusAndKey(candidates []routeCandidate, requestPath, alias string, excludeRoutes map[RouteKey]bool) []routeCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	filtered := make([]routeCandidate, 0, len(candidates))
	for _, c := range candidates {
		channel, ok := channelsIDM[c.channelId]
		if !ok {
			continue // channel not in cache
		}
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		// Check key availability
		if !isKeyEnabled(channel, c.keyIndex) {
			continue
		}
		// Exclude routes by RouteKey
		if excludeRoutes != nil {
			if excludeRoutes[RouteKey{ChannelId: c.channelId, KeyIndex: c.keyIndex, Model: alias}] {
				continue
			}
		}
		// Hard-signal exclusion: skip disabled routes
		if !IsRouteSelectable(RouteKey{ChannelId: c.channelId, KeyIndex: c.keyIndex, Model: alias}) {
			continue
		}
		// Advanced Custom path filtering
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel2advancedCustomConfig[c.channelId]; config != nil {
				if !config.SupportsPathForModel(requestPath, alias) {
					continue
				}
			}
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// isKeyEnabled checks if a specific key index is enabled for the channel.
func isKeyEnabled(channel *Channel, keyIndex int) bool {
	if !channel.ChannelInfo.IsMultiKey {
		return keyIndex == 0
	}
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		return false
	}
	statusList := channel.ChannelInfo.MultiKeyStatusList
	if statusList == nil {
		return true
	}
	if status, ok := statusList[keyIndex]; ok {
		return status == common.ChannelStatusEnabled
	}
	return true // default to enabled if not specified
}

// RouteScore is the full breakdown of one candidate's final scheduling score, so
// an admin query can be recomputed by hand: final == base * correction, where
// base == routingBaseWeight(static) * quality * health.
type RouteScore struct {
	StaticWeight int
	BaseWeight   float64
	Quality      float64
	Health       float64
	Correction   float64
	Final        float64

	ExpectedShare float64
	ActualShare   float64
	Opportunities int
	Selections    int
}

// scoreCandidates computes the final score of every candidate for one pool.
//
// The score is a product of four bounded terms:
//
//	base_weight  routingBaseWeight(static_weight), so weight 0 stays selectable
//	quality      EWMA synthesis, clamped to [QualityFloor, QualityCeil]; neutral
//	             1.0 until MinSamples observations exist
//	health       #368 state multiplier: 1.0 healthy, calm/dormant derated,
//	             0.0 disabled — the only term allowed to reach zero
//	correction   share-deficit multiplier, 1.0 at convergence
//
// The division of labour matters: quality expresses a continuous preference and
// is floored well above zero, so a badly performing route loses share but never
// leaves the pool. Only the state machine ejects, and it does so by returning a
// zero health multiplier.
//
// The returned targets map is the base-score share of each candidate, which is
// both the input the correction compares against and the snapshot recorded into
// the window once a winner is chosen.
func scoreCandidates(pool routestats.PoolKey, candidates []routeCandidate, alias string) (map[routestats.RouteID]RouteScore, map[routestats.RouteID]float64) {
	cfg := routestats.GetRouteStatsSetting()
	scores := make(map[routestats.RouteID]RouteScore, len(candidates))
	targets := make(map[routestats.RouteID]float64, len(candidates))

	var baseTotal float64
	for _, c := range candidates {
		id := routestats.RouteID{ChannelID: c.channelId, KeyIndex: c.keyIndex, UpstreamModel: c.upstreamModel}
		health := RouteWeightMultiplier(RouteKey{ChannelId: c.channelId, KeyIndex: c.keyIndex, Model: alias})
		quality := 1.0
		if cfg != nil && cfg.Enabled {
			if h := routestats.GetHandle(routestats.RouteKey{
				Group:            pool.Group,
				PublicModelAlias: pool.PublicModelAlias,
				ChannelID:        c.channelId,
				KeyIndex:         c.keyIndex,
				UpstreamModel:    c.upstreamModel,
			}); h != nil {
				quality = h.Quality().Quality
			}
		}
		baseWeight := float64(routingBaseWeight(c.staticWeight))
		base := baseWeight * quality * health
		// A NaN or negative product would poison the cumulative pick below, where
		// it silently biases every later candidate. Degrade the single bad route to
		// zero instead of failing the whole selection.
		if math.IsNaN(base) || math.IsInf(base, 0) || base < 0 {
			base = 0
		}
		scores[id] = RouteScore{
			StaticWeight: c.staticWeight,
			BaseWeight:   baseWeight,
			Quality:      quality,
			Health:       health,
			Correction:   1.0,
			Final:        base,
		}
		if base > 0 {
			targets[id] = base
			baseTotal += base
		}
	}

	if baseTotal <= 0 {
		return scores, map[routestats.RouteID]float64{}
	}
	for id, base := range targets {
		targets[id] = base / baseTotal
	}

	for id, corr := range routestats.Corrections(pool, targets, cfg) {
		s := scores[id]
		s.Correction = corr.Correction
		s.ExpectedShare = corr.ExpectedShare
		s.ActualShare = corr.ActualShare
		s.Opportunities = corr.Opportunities
		s.Selections = corr.Selections
		final := s.Final * corr.Correction
		if math.IsNaN(final) || math.IsInf(final, 0) || final < 0 {
			final = 0
		}
		s.Final = final
		scores[id] = s
	}
	return scores, targets
}

// selectByWeight performs weighted random selection over the final scores and
// records the winner into the pool's share window.
// rnd is the random source; if nil, uses global rand.
func selectByWeight(pool routestats.PoolKey, candidates []routeCandidate, alias string, rnd *rand.Rand) *routeCandidate {
	if len(candidates) == 0 {
		return nil
	}
	// A single candidate is returned as-is: with nothing to compete against, both
	// quality and the share correction would only scale a share that is already
	// 100%. It is still recorded below so the window reflects served traffic.
	if len(candidates) == 1 {
		only := candidates[0]
		id := routestats.RouteID{ChannelID: only.channelId, KeyIndex: only.keyIndex, UpstreamModel: only.upstreamModel}
		if RouteWeightMultiplier(RouteKey{ChannelId: only.channelId, KeyIndex: only.keyIndex, Model: alias}) <= 0 {
			return nil
		}
		routestats.RecordSelection(pool, id, map[routestats.RouteID]float64{id: 1.0}, routestats.GetRouteStatsSetting())
		return &candidates[0]
	}

	scores, targets := scoreCandidates(pool, candidates, alias)

	type weightedCandidate struct {
		candidate routeCandidate
		id        routestats.RouteID
		weight    float64
	}
	var weighted []weightedCandidate
	var totalWeight float64
	for _, c := range candidates {
		id := routestats.RouteID{ChannelID: c.channelId, KeyIndex: c.keyIndex, UpstreamModel: c.upstreamModel}
		w := scores[id].Final
		if w <= 0 {
			continue
		}
		weighted = append(weighted, weightedCandidate{candidate: c, id: id, weight: w})
		totalWeight += w
	}

	if totalWeight <= 0 || len(weighted) == 0 {
		return nil
	}

	r := rand.Float64()
	if rnd != nil {
		r = rnd.Float64()
	}
	randomWeight := r * totalWeight
	cfg := routestats.GetRouteStatsSetting()
	for i, wc := range weighted {
		randomWeight -= wc.weight
		if randomWeight < 0 || i == len(weighted)-1 {
			routestats.RecordSelection(pool, wc.id, targets, cfg)
			return &weighted[i].candidate
		}
	}
	last := weighted[len(weighted)-1]
	routestats.RecordSelection(pool, last.id, targets, cfg)
	return &last.candidate
}

// SelectRouteUnit is the unified entry point for route unit selection.
// It simultaneously determines channel, key, and upstream model.
// rnd is a deterministic random source; nil uses global rand.
func SelectRouteUnit(group string, alias string, requestPath string, retry int, excludeRoutes map[RouteKey]bool, rnd *rand.Rand) (*SelectedRoute, error) {
	// The retry parameter in the old priority-tier system drove tier descent.
	// In the new flat route-unit model, there are no priority tiers — all enabled
	// route units for the (group, alias) compete directly. The retry parameter
	// is retained for API compatibility but has no effect on selection.
	_ = retry

	var candidates []routeCandidate
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		candidates = getCandidatesFromCache(group, alias)
		channelSyncLock.RUnlock()
	} else {
		candidates = getCandidatesFromDB(group, alias)
	}

	// Normalize alias: try exact match first, then normalized model name
	if len(candidates) == 0 {
		normalizedAlias := ratio_setting.FormatMatchingModelName(alias)
		if normalizedAlias != alias {
			if common.MemoryCacheEnabled {
				channelSyncLock.RLock()
				candidates = getCandidatesFromCache(group, normalizedAlias)
				channelSyncLock.RUnlock()
			} else {
				candidates = getCandidatesFromDB(group, normalizedAlias)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Filter by channel status, key availability, excludeRoutes, and path
	candidates = filterCandidatesByChannelStatusAndKey(candidates, requestPath, alias, excludeRoutes)
	if len(candidates) == 0 {
		return nil, nil
	}

	// The share window is scoped to the competing pool, which is exactly the
	// (group, alias) pair selection draws from.
	pool := routestats.PoolKey{Group: group, PublicModelAlias: alias}
	selected := selectByWeight(pool, candidates, alias, rnd)
	if selected == nil {
		return nil, nil
	}

	// Resolve full channel and key
	channelSyncLock.RLock()
	channel := channelsIDM[selected.channelId]
	channelSyncLock.RUnlock()

	if channel == nil {
		return nil, nil
	}

	key, _, err := channel.GetNextEnabledKeyForIndex(selected.keyIndex)
	if err != nil {
		// If the specific key index is not available, fall back to any enabled key
		key, _, _ = channel.GetNextEnabledKey(alias)
	}

	// Create routestats handle for this route unit (per-attempt attribution)
	// RouteKey uses: Group (group), PublicModelAlias (alias = requested alias),
	// ChannelID, KeyIndex, UpstreamModel (from route row, not adapted).
	routeKey := routestats.RouteKey{
		Group:            group,
		PublicModelAlias: alias,
		ChannelID:        selected.channelId,
		KeyIndex:         selected.keyIndex,
		UpstreamModel:    selected.upstreamModel,
	}
	statsHandle := routestats.GetOrCreateHandle(routeKey)

	return &SelectedRoute{
		RouteId:       selected.routeId,
		Group:         group,
		Alias:         alias,
		Channel:       channel,
		ChannelId:     selected.channelId,
		KeyIndex:      selected.keyIndex,
		Key:           key,
		UpstreamModel: selected.upstreamModel,
		StatsHandle:   statsHandle,
	}, nil
}

// GetNextEnabledKeyForIndex returns the key at the specific index if enabled.
// Added to support route-unit selection where key_index is pre-determined.
func (channel *Channel) GetNextEnabledKeyForIndex(keyIndex int) (string, int, *types.NewAPIError) {
	if !channel.ChannelInfo.IsMultiKey {
		if keyIndex == 0 {
			return channel.Key, 0, nil
		}
		return "", 0, types.NewError(errors.New("invalid key index for single-key channel"), types.ErrorCodeChannelNoAvailableKey)
	}
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		return "", 0, types.NewError(errors.New("key index out of range"), types.ErrorCodeChannelNoAvailableKey)
	}
	statusList := channel.ChannelInfo.MultiKeyStatusList
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}
	if getStatus(keyIndex) != common.ChannelStatusEnabled {
		return "", 0, types.NewError(errors.New("key at index is disabled"), types.ErrorCodeChannelNoAvailableKey)
	}
	return keys[keyIndex], keyIndex, nil
}

// SelectedRouteFromChannel constructs a SelectedRoute from a specific channel
// for paths that serve real traffic without a weighted-random draw: channel
// affinity, specific-channel requests and locked replay. Their requests are
// folded into the pool's share window, because the correction is blind to skew
// it cannot see.
//
// It picks one enabled key via GetNextEnabledKey() and derives Group from
// the channel's first enabled group (via ExpandChannelModelRoutes) or empty string.
func SelectedRouteFromChannel(channel *Channel, alias string) (*SelectedRoute, error) {
	return selectedRouteFromChannel(channel, alias, true)
}

// SelectedRouteForProbe is the same construction for administrative probes
// (channel test, key probe). These requests are not user traffic: counting them
// would let a single "test all channels" click move the share window and make the
// correction chase load that no user generated.
func SelectedRouteForProbe(channel *Channel, alias string) (*SelectedRoute, error) {
	return selectedRouteFromChannel(channel, alias, false)
}

func selectedRouteFromChannel(channel *Channel, alias string, recordShare bool) (*SelectedRoute, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	key, keyIndex, err := channel.GetNextEnabledKey(alias)
	if err != nil {
		return nil, err
	}
	// Locate the route row this (channel, alias) pair corresponds to. Affinity and
	// specific-channel requests bypass weighted random selection, but they still
	// serve a real route unit, so their samples must be attributed to it. Without
	// this lookup the request would either go unattributed or, worse, be charged
	// against a key derived from the alias instead of the route's own upstream
	// model, which is a different route unit entirely.
	group := ""
	upstreamModel := alias
	routeId := 0
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		for g, aliasMap := range group2alias2routes {
			for _, rc := range aliasMap[alias] {
				if rc.channelId == channel.Id && rc.keyIndex == keyIndex {
					group, upstreamModel, routeId = g, rc.upstreamModel, rc.routeId
					break
				}
			}
			if group != "" {
				break
			}
		}
		channelSyncLock.RUnlock()
	} else {
		var row ChannelModelRoute
		if err := DB.Where(commonGroupCol+" IS NOT NULL AND public_model_alias = ? AND channel_id = ? AND key_index = ? AND enabled = ?",
			alias, channel.Id, keyIndex, true).First(&row).Error; err == nil {
			group, upstreamModel, routeId = row.Group, row.UpstreamModel, row.Id
		}
	}

	var statsHandle *routestats.RouteHandle
	if routeId != 0 {
		statsHandle = routestats.GetOrCreateHandle(routestats.RouteKey{
			Group:            group,
			PublicModelAlias: alias,
			ChannelID:        channel.Id,
			KeyIndex:         keyIndex,
			UpstreamModel:    upstreamModel,
		})
		// This path bypasses weighted random selection entirely, yet it still
		// consumes traffic from the pool. The share window has to see it, or the
		// correction is blind to exactly the skew it exists to fix: with 30% of a
		// three-route pool pinned here by channel affinity, the pinned route takes
		// 53.4% instead of 33.3%, and leaving these requests unrecorded measurably
		// degrades the balancer back to that baseline.
		//
		// The recorded entitlement is the pool's full candidate set as scored right
		// now: affinity did not run a competition, so the counterfactual share is
		// what the other routes would have been entitled to.
		//
		// Probes are excluded: an administrator testing every channel would
		// otherwise inject one entry per channel into the window and the correction
		// would spend the next window compensating for traffic no user sent.
		if recordShare {
			recordBypassSelection(group, alias, routestats.RouteID{
				ChannelID:     channel.Id,
				KeyIndex:      keyIndex,
				UpstreamModel: upstreamModel,
			})
		}
	}

	return &SelectedRoute{
		RouteId:       routeId,
		Group:         group,
		Alias:         alias,
		Channel:       channel,
		ChannelId:     channel.Id,
		KeyIndex:      keyIndex,
		Key:           key,
		UpstreamModel: upstreamModel,
		StatsHandle:   statsHandle,
	}, nil
}

// recordBypassSelection folds a selection made outside weighted random into the
// pool's share window, using the pool's current base-score distribution as the
// entitlement snapshot. Candidates are read without status/key filtering because
// this is an accounting entry, not a selection: a route that is momentarily
// unselectable was still entitled to its configured share of this request.
func recordBypassSelection(group, alias string, selected routestats.RouteID) {
	cfg := routestats.GetRouteStatsSetting()
	if cfg == nil || !cfg.Enabled || cfg.ShareWindowSize <= 0 {
		return
	}
	var candidates []routeCandidate
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		candidates = getCandidatesFromCache(group, alias)
		channelSyncLock.RUnlock()
	} else {
		candidates = getCandidatesFromDB(group, alias)
	}
	if len(candidates) == 0 {
		return
	}
	pool := routestats.PoolKey{Group: group, PublicModelAlias: alias}
	_, targets := scoreCandidates(pool, candidates, alias)
	if len(targets) == 0 {
		// Every candidate scored zero (for example the whole pool is disabled).
		// Charge the served route alone rather than dropping the request.
		targets = map[routestats.RouteID]float64{selected: 1.0}
	} else if _, ok := targets[selected]; !ok {
		// The served route scored zero but was still used: it must appear in its own
		// entry, otherwise its selection count would exceed its opportunity count.
		targets[selected] = 0
	}
	routestats.RecordSelection(pool, selected, targets, cfg)
}
