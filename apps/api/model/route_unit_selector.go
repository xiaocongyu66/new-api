package model

import (
	"errors"
	"math/rand/v2"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/routestats"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type RouteKey struct {
	ChannelId int
	KeyIndex  int
}

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
			if excludeRoutes[RouteKey{ChannelId: c.channelId, KeyIndex: c.keyIndex}] {
				continue
			}
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

// selectByWeight performs weighted random selection on candidates using health-adjusted weights.
// rnd is the random source; if nil, uses global rand.
func selectByWeight(candidates []routeCandidate, rnd *rand.Rand) *routeCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return &candidates[0]
	}

	healthMgr := GetChannelHealthManager()
	maxEjectionPercent := 0
	if cfg := operation_setting.GetChannelHealthSetting(); cfg != nil {
		maxEjectionPercent = cfg.CooldownMaxEjectionPercent
	}

	channelIDs := make([]int, 0, len(candidates))
	for _, c := range candidates {
		channelIDs = append(channelIDs, c.channelId)
	}
	ejected := healthMgr.FilterCoolingChannels(channelIDs, maxEjectionPercent)

	// Build final candidate list with weights, excluding ejected
	type weightedCandidate struct {
		candidate routeCandidate
		weight    float64
	}
	var weighted []weightedCandidate
	var totalWeight float64
	for _, c := range candidates {
		if ejected != nil && ejected[c.channelId] {
			continue
		}
		effW := healthMgr.routingWeight(c.channelId, routingBaseWeight(c.staticWeight), true)
		if effW <= 0 {
			continue
		}
		weighted = append(weighted, weightedCandidate{candidate: c, weight: effW})
		totalWeight += effW
	}

	if totalWeight <= 0 || len(weighted) == 0 {
		return nil
	}

	r := rand.Float64()
	if rnd != nil {
		r = rnd.Float64()
	}
	randomWeight := r * totalWeight
	for i, wc := range weighted {
		randomWeight -= wc.weight
		if randomWeight < 0 || i == len(weighted)-1 {
			return &wc.candidate
		}
	}
	return &weighted[len(weighted)-1].candidate
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

	// Weighted random selection
	selected := selectByWeight(candidates, rnd)
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
		key, _, _ = channel.GetNextEnabledKey()
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
// for locked-channel replay paths (channel test, task locked replay).
// It picks one enabled key via GetNextEnabledKey() and derives Group from
// the channel's first enabled group (via ExpandChannelModelRoutes) or empty string.
func SelectedRouteFromChannel(channel *Channel, alias string) (*SelectedRoute, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	key, keyIndex, err := channel.GetNextEnabledKey()
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
