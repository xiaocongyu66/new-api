package channel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/common"
)

// withChannelCacheFixture installs an isolated in-memory channel cache and
// restores the previous globals afterwards, so these tests cannot leak state into
// the rest of the package.
//
// GetRandomSatisfiedChannel resolves a route unit through SelectRouteUnit, which
// reads group2alias2routes and channelsIDM under channelSyncLock and only takes
// the memory-cache path when common.MemoryCacheEnabled is true.
//
// routeWeights maps channel id to the static weight the selector must see. A
// channel carries no scheduling weight of its own any more, so the fixture states
// the weight where production reads it: on the route unit. Every channel gets one
// single-key route whose id equals the channel id.
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
	aliasRoutes := make(map[string]map[string][]routeCandidate)
	for _, ch := range channels {
		ids = append(ids, ch.Id)
		idm[ch.Id] = ch
		if aliasRoutes[group] == nil {
			aliasRoutes[group] = make(map[string][]routeCandidate)
		}
		aliasRoutes[group][modelName] = append(aliasRoutes[group][modelName], routeCandidate{
			routeId:       ch.Id,
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
	return &Channel{Id: id, Status: common.ChannelStatusEnabled, Key: "sk-test"}
}

// TestSingleChannelShortCircuitIgnoresWeight pins a deliberate asymmetry: with a
// single candidate, selection returns it without consulting weight
// (selectByWeight's len==1 branch). There is nothing to fall back to, so
// selecting it is correct — but it means weight=0 behaves differently here than
// in the multi-candidate path, where routingBaseWeight maps it to 1. Locking this
// down so a later refactor does not silently start dropping single-route groups.
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
// bypass request-level exclusion, otherwise a route that just failed would be
// handed back to the same request.
func TestSingleChannelShortCircuitRespectsExcludeSet(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	only := testChannel(9102)
	withChannelCacheFixture(t, []*Channel{only}, group, modelName, map[int]int{9102: 10})

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "",
		map[RouteKey]bool{{ChannelId: only.Id, KeyIndex: 0, Model: modelName}: true})
	require.NoError(t, err)
	assert.Nil(t, got, "the only route was excluded, so selection must yield nothing")
}

// TestExcludeSetIsPerRouteUnitNotPerChannel is the exclusion granularity the
// route-unit model turns on, and the one thing a channel-keyed exclude set could
// not express: one multi-key channel owns several independently retryable route
// units, so a failure on key 0 must eject exactly that unit and leave key 1
// serving. Collapsing the two onto their shared channel id would take a healthy
// key out of rotation on its sibling's failure, and after both keys had failed
// the pool would look empty a retry too early.
func TestExcludeSetIsPerRouteUnitNotPerChannel(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	// One channel, two keys, therefore two route units sharing a channel id.
	ch := &Channel{
		Id:     9110,
		Status: common.ChannelStatusEnabled,
		Key:    "sk-key0\nsk-key1",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	withChannelCacheFixture(t, []*Channel{ch}, group, modelName, map[int]int{9110: 100})
	channelSyncLock.Lock()
	group2alias2routes[group][modelName] = []routeCandidate{
		{routeId: 1, channelId: 9110, keyIndex: 0, upstreamModel: modelName, staticWeight: 100},
		{routeId: 2, channelId: 9110, keyIndex: 1, upstreamModel: modelName, staticWeight: 100},
	}
	channelSyncLock.Unlock()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	firstKey := RouteKey{ChannelId: 9110, KeyIndex: 0, Model: modelName}
	secondKey := RouteKey{ChannelId: 9110, KeyIndex: 1, Model: modelName}

	// Excluding one unit must leave its sibling on the same channel selectable.
	for range 20 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{firstKey: true})
		require.NoError(t, err)
		require.NotNil(t, got, "the sibling key on the same channel is still available")
		assert.Equal(t, 9110, got.ChannelId)
		assert.Equal(t, 1, got.KeyIndex, "only the excluded key index may be skipped")
	}

	// And the mirror case, so the assertion cannot pass by always picking key 1.
	for range 20 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{secondKey: true})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 0, got.KeyIndex)
	}

	// Only when both units are excluded is the pool actually empty.
	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "",
		map[RouteKey]bool{firstKey: true, secondKey: true})
	require.NoError(t, err)
	assert.Nil(t, got, "with every route unit excluded there is nothing left to serve")
}

// TestWeightDecidesShareWithoutPriorityTiers pins what replaced the priority
// walk. Retry used to descend priority tiers, so a lower-priority channel was
// unreachable until the request had already failed once. Route units are flat:
// every unit for a (group, alias) competes on weight in one pool, and retry no
// longer moves between tiers. A refactor that reintroduced tiering would starve
// the low-weight routes here at retry 0.
func TestWeightDecidesShareWithoutPriorityTiers(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	channels := []*Channel{testChannel(9201), testChannel(9202), testChannel(9203)}
	withChannelCacheFixture(t, channels, group, modelName, map[int]int{
		9201: 100,
		9202: 10,
		9203: 50,
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

	// Weight order must be reflected in share order.
	assert.Greater(t, counts[9201], counts[9203], "weight 100 must beat weight 50")
	assert.Greater(t, counts[9203], counts[9202], "weight 50 must beat weight 10")

	// Every route stays reachable at retry 0: in the old tier model the two
	// lower-priority channels would have been unreachable until a retry.
	assert.Positive(t, counts[9202], "the lightest route is still in the pool at retry 0")
	assert.Positive(t, counts[9203])

	// Retry does not move the pool any more, so a retried request draws from the
	// same candidate set rather than descending to a different tier.
	retried := map[int]int{}
	for range 300 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 7, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		retried[got.ChannelId]++
	}
	assert.Positive(t, retried[9201], "retry must not exclude the heaviest route")
	assert.Greater(t, retried[9201], retried[9202],
		"weight still decides the split on a retried request")
}

// TestHealthDeratesWithinTheFlatPool documents that health scoring reduces a
// route's share without ejecting it: an isolated route loses traffic to its peers
// but stays selectable, because only the disabled state leaves the candidate set.
// Cross-tier promotion no longer exists to confound this, so the derating is
// observable directly in the one pool.
func TestHealthDeratesWithinTheFlatPool(t *testing.T) {
	const group, modelName = "edge-group", "edge-model"

	healthy := testChannel(9301)
	isolated := testChannel(9302)
	withChannelCacheFixture(t, []*Channel{healthy, isolated}, group, modelName,
		map[int]int{healthy.Id: 100, isolated.Id: 100})

	withRouteHealthDB(t)
	withHealthSetting(t, DefaultChannelModelHealthSetting())

	isolatedKey := RouteKey{ChannelId: isolated.Id, KeyIndex: 0, Model: modelName}
	// The selector reads the real clock, so the isolation window must be live.
	require.NoError(t, RecordRetryableFailure(isolatedKey, "bad_response", FailureSourceUpstream, time.Now()))
	require.Less(t, RouteWeightMultiplier(isolatedKey), 1.0,
		"the fixture must actually have derated the route")

	counts := map[int]int{}
	for range 400 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	assert.Greater(t, counts[healthy.Id], counts[isolated.Id],
		"within the pool, the healthy route must win the majority of selections")
	assert.Less(t, counts[isolated.Id], counts[healthy.Id]*3/4,
		"the isolated route is derated, not merely tied")
	assert.Positive(t, counts[isolated.Id],
		"a derated route keeps a reduced share: only DisableRoute ejects")

	// Disabling is the one state that removes it from the pool entirely.
	require.NoError(t, DisableRoute(isolatedKey, time.Now()))
	after := map[int]int{}
	for range 100 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		after[got.ChannelId]++
	}
	assert.Zero(t, after[isolated.Id], "a disabled route must never be selected")
	assert.Equal(t, 100, after[healthy.Id])
}
