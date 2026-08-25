package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// withChannelCacheFixture installs an isolated in-memory channel cache and
// restores the previous globals afterwards, so these tests cannot leak state into
// the rest of the package.
//
// GetRandomSatisfiedChannel reads group2model2channels and channelsIDM under
// channelSyncLock, and only takes the memory-cache path when
// common.MemoryCacheEnabled is true (channel_cache.go:114).
// routeWeights maps channel id to the route unit static weight the selector should
// see. Channels no longer carry a weight of their own, so the test states the
// scheduling weight where production reads it: on the route unit.
func withChannelCacheFixture(t *testing.T, channels []*Channel, group, modelName string, routeWeights map[int]int) {
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
		aliasRoutes[group][modelName] = append(aliasRoutes[group][modelName], routeCandidate{
			routeId:       ch.Id, // use channel ID as route ID for tests
			channelId:     ch.Id,
			keyIndex:      0,
			upstreamModel: modelName,
			staticWeight:  routeWeights[ch.Id],
		})
	}

	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{group: {modelName: ids}}
	channelsIDM = idm
	group2alias2routes = aliasRoutes
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
}

func testChannel(id int) *Channel {
	return &Channel{Id: id, Status: common.ChannelStatusEnabled}
}

// TestSingleChannelShortCircuitIgnoresWeight pins a deliberate asymmetry: with a
// single candidate, GetRandomSatisfiedChannel returns early without consulting
// weight. There is nothing to fall back to, so selecting it is correct — but it
// means weight=0 behaves differently here than in the multi-channel path, where
// routingBaseWeight maps it to 1. Locking this down so a later refactor does not
// silently start dropping single-channel groups.
func TestSingleChannelShortCircuitIgnoresWeight(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	// weight=0 would be the least attractive route possible in the weighted path.
	only := testChannel(9101)
	withChannelCacheFixture(t, []*Channel{only}, group, modelName, map[int]int{9101: 0})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

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

	only := testChannel(9102)
	withChannelCacheFixture(t, []*Channel{only}, group, modelName, map[int]int{9102: 10})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: only.Id, KeyIndex: 0, Model: modelName}: true})
	require.NoError(t, err)
	assert.Nil(t, got, "the only channel was excluded, so selection must yield nothing")
}

// TestWeightBasedSelection verifies that in the new route-unit model, all routes
// for a (group, alias) compete directly by weight without priority tiers.
func TestWeightBasedSelection(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	channels := []*Channel{testChannel(9201), testChannel(9202), testChannel(9203)}
	withChannelCacheFixture(t, channels, group, modelName, map[int]int{
		9201: 100, // high weight
		9202: 10,  // low weight
		9203: 50,  // medium weight
	})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

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

// TestHealthScoreActsInFlatModel verifies that health scoring derates channels
// within the flat route-unit pool (no priority tiers).
func TestHealthScoreActsInFlatModel(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	healthy := testChannel(9301)
	isolated := testChannel(9302)
	withChannelCacheFixture(t, []*Channel{healthy, isolated}, group, modelName,
		map[int]int{healthy.Id: 100, isolated.Id: 100})

	withRouteHealthDB(t)
	cfg := operation_setting.DefaultChannelModelHealthSetting()
	withHealthSetting(t, cfg)

	// The selectors read the real clock, so the isolation window must be live.
	require.NoError(t, RecordRetryableFailure(RouteKey{ChannelId: isolated.Id, KeyIndex: 0, Model: modelName}, "bad_response", FailureSourceUpstream, time.Now()))

	// In the flat model, both channels compete directly; isolated should rarely win
	counts := map[int]int{}
	for range 400 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	assert.Greater(t, counts[healthy.Id], counts[isolated.Id],
		"within the pool, the healthy channel must win the majority of selections")
	assert.Less(t, counts[isolated.Id], counts[healthy.Id]*3/4,
		"the isolated channel is derated (calm state at 50% weight)")
}

// TestGetNextEnabledKeySkipsIsolatedKey covers the Wave B execution contract: a
// channel is selected as a whole, but the key picked inside it must avoid the
// isolated key index while a healthy sibling key exists.
func TestGetNextEnabledKeySkipsIsolatedKey(t *testing.T) {
	withRouteHealthDB(t)
	withHealthSetting(t, operation_setting.DefaultChannelModelHealthSetting())

	channel := &Channel{
		Id:  9124,
		Key: "key-a\nkey-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}
	now := time.Now()
	require.NoError(t, RecordRetryableFailure(RouteKey{ChannelId: channel.Id, KeyIndex: 0, Model: "key-model"}, "bad_response", FailureSourceUpstream, now))

	for range 8 {
		key, index, apiErr := channel.GetNextEnabledKey("key-model")
		// apiErr is a typed pointer, so it must be compared with Nil rather than
		// NoError: a nil *types.NewAPIError still yields a non-nil error interface.
		require.Nil(t, apiErr)
		assert.Equal(t, 1, index, "the isolated key index must lose every pick")
		assert.Equal(t, "key-b", key)
	}
}
