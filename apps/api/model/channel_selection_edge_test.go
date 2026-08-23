package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
)

// withChannelCacheFixture installs an isolated in-memory channel cache and
// restores the previous globals afterwards, so these tests cannot leak state into
// the rest of the package.
//
// GetRandomSatisfiedChannel reads group2model2channels and channelsIDM under
// channelSyncLock, and only takes the memory-cache path when
// common.MemoryCacheEnabled is true (channel_cache.go:114).
func withChannelCacheFixture(t *testing.T, channels []*Channel, group, modelName string) {
	t.Helper()

	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := group2alias2routes
	prevMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		group2alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	})

	ids := make([]int, 0, len(channels))
	idm := make(map[int]*Channel, len(channels))
	// Build route candidates for the new selector
	aliasRoutes := make(map[string]map[string][]routeCandidate)
	for _, ch := range channels {
		ids = append(ids, ch.Id)
		idm[ch.Id] = ch
		// Create route candidates for each key (single key for test channels)
		if aliasRoutes[group] == nil {
			aliasRoutes[group] = make(map[string][]routeCandidate)
		}
		weight := 100
		if ch.Weight != nil {
			weight = int(*ch.Weight)
		}
		aliasRoutes[group][modelName] = append(aliasRoutes[group][modelName], routeCandidate{
			routeId:       ch.Id, // use channel ID as route ID for tests
			channelId:     ch.Id,
			keyIndex:      0,
			upstreamModel: modelName,
			staticWeight:  weight,
		})
	}

	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{group: {modelName: ids}}
	channelsIDM = idm
	group2alias2routes = aliasRoutes
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
}

func testChannel(id int, weight uint, priority int64) *Channel {
	w, p := weight, priority
	status := common.ChannelStatusEnabled
	return &Channel{Id: id, Weight: &w, Priority: &p, Status: status}
}
func TestSingleChannelShortCircuitIgnoresWeight(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	// weight=0 plus a floored health score would be the least attractive channel
	// possible in the weighted path.
	only := testChannel(9101, 0, 7)
	withChannelCacheFixture(t, []*Channel{only}, group, modelName)

	mgr := resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)
	for range 50 {
		mgr.RecordChannelOutcome(only.Id, OutcomeFatal)
	}
	require.InDelta(t, 0.05, mgr.GetScore(only.Id), 1e-9, "channel is scored at the floor")

	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
	require.NoError(t, err)
	require.NotNil(t, got, "a single candidate must still be selected")
	assert.Equal(t, only.Id, got.ChannelId)
}

// TestSingleChannelShortCircuitRespectsExcludeSet: the short circuit must not
// bypass request-level exclusion, otherwise a channel that just failed would be
// handed back to the same request.
func TestSingleChannelShortCircuitRespectsExcludeSet(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	only := testChannel(9102, 10, 7)
	withChannelCacheFixture(t, []*Channel{only}, group, modelName)
	resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)

	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: only.Id, KeyIndex: 0}: true})
	require.NoError(t, err)
	assert.Nil(t, got, "the only channel was excluded, so selection must yield nothing")
}

// TestRetryDescendsPriorityTiers pins the priority semantics that retry drives.
// sortedUniquePriorities is sorted with sort.Reverse (channel_cache.go:154), so
// index 0 is the NUMERICALLY HIGHEST priority and retry walks downward. The clamp
// TestWeightBasedSelection verifies that in the new route-unit model, all routes
// for a (group, alias) compete directly by weight without priority tiers.
func TestWeightBasedSelection(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	// Channels with different weights, all same priority (no tiers in new model)
	channels := []*Channel{
		testChannel(9201, 100, 100), // high weight
		testChannel(9202, 10, 100),  // low weight
		testChannel(9203, 50, 100),  // medium weight
	}
	withChannelCacheFixture(t, channels, group, modelName)
	resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)

	counts := map[int]int{}
	for range 1000 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	// Highest weight (9201) should get the most selections
	assert.Greater(t, counts[9201], counts[9202])
	assert.Greater(t, counts[9201], counts[9203])
}

// TestHealthScoreActsWithinPriorityTierOnly documents that health scoring is
// tier-local: a degraded channel loses share to its peers at the same priority,
// TestHealthScoreActsInFlatModel verifies that health scoring derates channels
// within the flat route-unit pool (no priority tiers).
func TestHealthScoreActsInFlatModel(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	healthy := testChannel(9301, 10, 100)
	degraded := testChannel(9302, 10, 100)
	withChannelCacheFixture(t, []*Channel{healthy, degraded}, group, modelName)

	mgr := resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)
	for range 50 {
		mgr.RecordChannelOutcome(degraded.Id, OutcomeFatal)
	}
	require.InDelta(t, 0.05, mgr.GetScore(degraded.Id), 1e-9)
	require.Equal(t, 1.0, mgr.GetScore(healthy.Id))

	// In the flat model, both channels compete directly; degraded should rarely win
	counts := map[int]int{}
	for range 400 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	assert.Greater(t, counts[healthy.Id], counts[degraded.Id],
		"within the pool, the healthy channel must win the majority of selections")
	assert.Less(t, counts[degraded.Id], counts[healthy.Id]/4,
		"the degraded channel is heavily derated")
}
