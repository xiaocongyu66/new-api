package model

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/routestats"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func withRouteUnitFixture(t *testing.T, channels []*Channel, group, alias string, routes []ChannelModelRoute) func() {
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
	advancedCustomConfig := make(map[int]*dto.AdvancedCustomConfig)
	for _, ch := range channels {
		ids = append(ids, ch.Id)
		idm[ch.Id] = ch
		if ch.Type == constant.ChannelTypeAdvancedCustom {
			if config := ch.GetOtherSettings().AdvancedCustom; config != nil {
				advancedCustomConfig[ch.Id] = config
			}
		}
	}

	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{group: {alias: ids}}
	channelsIDM = idm
	channel2advancedCustomConfig = advancedCustomConfig

	// Build group2alias2routes from provided routes
	group2alias2routes = make(map[string]map[string][]routeCandidate)
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
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	return func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		group2alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	}
}

func testRouteChannel(id int, weight uint, priority int64, isMultiKey bool, keys []string, keyStatus map[int]int) *Channel {
	w, p := weight, priority
	ch := &Channel{
		Id:       id,
		Weight:   &w,
		Priority: &p,
		Status:   common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:         isMultiKey,
			MultiKeyStatusList: keyStatus,
		},
	}
	if isMultiKey {
		// Store keys in channel.Key as JSON array
		if len(keys) > 0 {
			ch.Key = `["` + keys[0] + `"`
			for i := 1; i < len(keys); i++ {
				ch.Key += `,"` + keys[i] + `"`
			}
			ch.Key += `]`
		}
	} else if len(keys) > 0 {
		ch.Key = keys[0]
	}
	return ch
}

func testRoute(routeId, channelId, keyIndex int, group, alias, upstreamModel string, weight int) ChannelModelRoute {
	return ChannelModelRoute{
		Id:               routeId,
		Group:            group,
		PublicModelAlias: alias,
		ChannelId:        channelId,
		KeyIndex:         keyIndex,
		UpstreamModel:    upstreamModel,
		StaticWeight:     weight,
		Enabled:          true,
	}
}

func TestSelectRouteUnit_SingleCandidate(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch := testRouteChannel(1001, 10, 5, false, []string{"sk-single"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1001, 0, group, alias, "upstream-model", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	rnd := rand.New(rand.NewPCG(42, 0))
	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1, selected.RouteId)
	assert.Equal(t, 1001, selected.ChannelId)
	assert.Equal(t, 0, selected.KeyIndex)
	assert.Equal(t, "sk-single", selected.Key)
	assert.Equal(t, "upstream-model", selected.UpstreamModel)
}
func TestSelectRouteUnit_MultiKeyChannel(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch := testRouteChannel(1002, 10, 5, true, []string{"sk-key0", "sk-key1", "sk-key2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1002, 0, group, alias, "upstream-a", 100),
		testRoute(2, 1002, 1, group, alias, "upstream-b", 100),
		testRoute(3, 1002, 2, group, alias, "upstream-c", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	// Run many times - all three key indices should be selected with roughly equal probability
	rnd := rand.New(rand.NewPCG(100, 0))
	counts := make(map[int]int)
	keyMap := make(map[int]string)
	for range 300 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.KeyIndex]++
		keyMap[selected.KeyIndex] = selected.Key
	}

	// All three key indices should be selected
	assert.Equal(t, 3, len(counts))
	assert.Contains(t, counts, 0)
	assert.Contains(t, counts, 1)
	assert.Contains(t, counts, 2)

	// Verify correct key is returned for each key index (keys are stored as JSON, so they include quotes)
	assert.Equal(t, "\"sk-key0\"", keyMap[0])
	assert.Equal(t, "\"sk-key1\"", keyMap[1])
	assert.Equal(t, "\"sk-key2\"", keyMap[2])
}

func TestSelectRouteUnit_MultiKeyDisabledKeyExcluded(t *testing.T) {
	const group, alias = "test-group", "test-model"

	// Key index 1 is disabled
	keyStatus := map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusEnabled}
	ch := testRouteChannel(1003, 10, 5, true, []string{"sk-key0", "sk-key1", "sk-key2"}, keyStatus)
	routes := []ChannelModelRoute{
		testRoute(1, 1003, 0, group, alias, "upstream-a", 100),
		testRoute(2, 1003, 1, group, alias, "upstream-b", 100),
		testRoute(3, 1003, 2, group, alias, "upstream-c", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	// Run many times - key index 1 should never be selected
	rnd := rand.New(rand.NewPCG(200, 0))
	counts := make(map[int]int)
	for range 100 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.KeyIndex]++
	}
	assert.Equal(t, 0, counts[1], "disabled key index 1 should never be selected")
	assert.Greater(t, counts[0], 0)
	assert.Greater(t, counts[2], 0)
}

func TestSelectRouteUnit_ExcludeRoutes(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch1 := testRouteChannel(1004, 10, 5, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1005, 10, 5, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1004, 0, group, alias, "upstream-1", 100),
		testRoute(2, 1005, 0, group, alias, "upstream-2", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	// Exclude route 1 (channel 1004, keyIndex 0)
	excludeRoutes := map[RouteKey]bool{{ChannelId: 1004, KeyIndex: 0, Model: alias}: true}
	rnd := rand.New(rand.NewPCG(300, 0))
	selected, err := SelectRouteUnit(group, alias, "", 0, excludeRoutes, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1005, selected.ChannelId, "excluded route should not be selected")
}

func TestSelectRouteUnit_DisabledRouteExcluded(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch1 := testRouteChannel(1006, 10, 5, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1007, 10, 5, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1006, 0, group, alias, "upstream-1", 100),
		{Id: 2, Group: group, PublicModelAlias: alias, ChannelId: 1007, KeyIndex: 0, UpstreamModel: "upstream-2", StaticWeight: 100, Enabled: false}, // disabled
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	rnd := rand.New(rand.NewPCG(400, 0))
	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1006, selected.ChannelId, "disabled route should not be selected")
}

func TestSelectRouteUnit_CooldownEjection(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch1 := testRouteChannel(1008, 10, 5, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1009, 10, 5, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1008, 0, group, alias, "upstream-1", 100),
		testRoute(2, 1009, 0, group, alias, "upstream-2", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanup()

	// The state machine persists to the DB, so install one before driving it.
	withRouteHealthDB(t)
	ClearRouteHealthCache()

	// Push route 1008 into calm state via retryable failures
	now := time.Now()
	require.NoError(t, RecordRetryableFailure(RouteKey{ChannelId: 1008, KeyIndex: 0, Model: alias}, "bad_response", FailureSourceUpstream, now))

	// In the new model, a calm route stays selectable at reduced weight (0.5x),
	// while a disabled route is excluded entirely. A disabled route is the ONLY
	// state that leaves the candidate set.
	rnd := rand.New(rand.NewPCG(500, 0))
	counts := make(map[int]int)
	for range 200 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.ChannelId]++
	}
	// calm multiplier 0.5 pins base scores at 101:50.5 ≈ 2:1, and the share
	// correction holds actual traffic exactly there (133:67 of 200 ≈ 66.5%,
	// expected 66.7%). The correction suppresses the sampling jitter that used
	// to push the healthy route strictly past 2x, so dominance is asserted as a
	// band around the base-share split instead of strict 2x.
	assert.Greater(t, counts[1009], counts[1008], "healthy route must dominate calm route")
	assert.Greater(t, counts[1009], 120, "healthy route holds its base share (~2/3)")
	assert.Less(t, counts[1009], 147, "correction keeps healthy route near base share, not monopoly")

	// Now disable the calm route - it must be excluded entirely
	require.NoError(t, DisableRoute(RouteKey{ChannelId: 1008, KeyIndex: 0, Model: alias}, now))
	counts = make(map[int]int)
	for range 50 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.ChannelId]++
	}
	assert.Equal(t, 0, counts[1008], "disabled route must never be selected")
	assert.Equal(t, 50, counts[1009], "only healthy route remains")
}

func TestSelectRouteUnit_AdvancedCustomPathFilter(t *testing.T) {
	const group, alias = "test-group", "test-model"

	// Advanced Custom channel that only supports /v1/chat/completions
	ch1 := testRouteChannel(1010, 10, 5, false, []string{"sk-1"}, nil)
	ch1.Type = constant.ChannelTypeAdvancedCustom
	ch1.OtherSettings = `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","models":["test-model"]}]}}`

	ch2 := testRouteChannel(1011, 10, 5, false, []string{"sk-2"}, nil)
	ch2.Type = constant.ChannelTypeAdvancedCustom
	ch2.OtherSettings = `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/embeddings","models":["test-model"]}]}}`

	routes := []ChannelModelRoute{
		testRoute(1, 1010, 0, group, alias, "upstream-1", 100),
		testRoute(2, 1011, 0, group, alias, "upstream-2", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	// Request to /v1/chat/completions should only select ch1
	rnd := rand.New(rand.NewPCG(600, 0))
	selected, err := SelectRouteUnit(group, alias, "/v1/chat/completions", 0, nil, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1010, selected.ChannelId)

	// Request to /v1/embeddings should only select ch2
	selected, err = SelectRouteUnit(group, alias, "/v1/embeddings", 0, nil, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1011, selected.ChannelId)

	// Request to unknown path should select none
	selected, err = SelectRouteUnit(group, alias, "/v1/unknown", 0, nil, rnd)
	require.NoError(t, err)
	assert.Nil(t, selected)
}

func TestSelectRouteUnit_DeterministicCacheVsDB(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch1 := testRouteChannel(2001, 10, 5, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(2002, 20, 5, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 2001, 0, group, alias, "upstream-1", 100),
		testRoute(2, 2002, 0, group, alias, "upstream-2", 200), // higher weight
	}

	// Test with MemoryCacheEnabled = true (cache path)
	cleanupCache := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanupCache()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	rndCache := rand.New(rand.NewPCG(700, 0))
	rndDB := rand.New(rand.NewPCG(700, 0)) // same seed

	// Cache path
	common.MemoryCacheEnabled = true
	selectedCache, err := SelectRouteUnit(group, alias, "", 0, nil, rndCache)
	require.NoError(t, err)
	require.NotNil(t, selectedCache)

	// DB path (no routes in DB, should return nil)
	common.MemoryCacheEnabled = false
	selectedDB, err := SelectRouteUnit(group, alias, "", 0, nil, rndDB)
	require.NoError(t, err)
	// DB path returns nil because no routes in test DB
	// This is expected - the test verifies both paths don't crash
	assert.Nil(t, selectedDB)

	// Restore cache for cleanup
	common.MemoryCacheEnabled = true
}

func TestSelectRouteUnit_WeightDistribution(t *testing.T) {
	const group, alias = "test-group", "test-model"

	ch1 := testRouteChannel(3001, 10, 5, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(3002, 10, 5, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 3001, 0, group, alias, "upstream-1", 100), // weight 100
		testRoute(2, 3002, 0, group, alias, "upstream-2", 300), // weight 300 (3x)
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch1, ch2}, group, alias, routes)
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	rnd := rand.New(rand.NewPCG(800, 0))
	counts := make(map[int]int)
	for range 1000 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.ChannelId]++
	}

	// ch2 has 3x weight, should get ~75% of selections
	// Allow some variance
	assert.InDelta(t, 750, counts[3002], 100, "weight 300 should get ~75%")
	assert.InDelta(t, 250, counts[3001], 100, "weight 100 should get ~25%")
}

func TestSelectRouteUnit_NormalizedAliasFallback(t *testing.T) {
	const group = "test-group"
	const alias = "gemini-2.5-flash-thinking-512" // FormatMatchingModelName normalizes this

	const normalizedAlias = "gemini-2.5-flash-thinking-*"
	ch := testRouteChannel(4001, 10, 5, false, []string{"sk-1"}, nil)
	// Use fixture but with normalized alias for both group2model2channels and routes
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

	ids := []int{4001}
	idm := map[int]*Channel{4001: ch}
	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{group: {alias: ids}}
	channelsIDM = idm
	if group2alias2routes == nil {
		group2alias2routes = make(map[string]map[string][]routeCandidate)
	}
	group2alias2routes[group] = map[string][]routeCandidate{
		normalizedAlias: {
			{routeId: 1, channelId: 4001, keyIndex: 0, upstreamModel: "upstream-normalized", staticWeight: 100},
		},
	}
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	// Request with non-normalized alias should fall back to normalized
	rnd := rand.New(rand.NewPCG(900, 0))
	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, alias, selected.Alias) // SelectedRoute.Alias carries the requested model name
	assert.Equal(t, "upstream-normalized", selected.UpstreamModel)
}

func TestSelectRouteUnit_EmptyResult(t *testing.T) {
	const group, alias = "empty-group", "empty-model"

	cleanup := withRouteUnitFixture(t, []*Channel{}, group, alias, []ChannelModelRoute{})
	defer cleanup()

	rnd := rand.New(rand.NewPCG(1000, 0))
	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
	require.NoError(t, err)
	assert.Nil(t, selected)
}

// TestSelectRouteUnitAttachesStatsHandleWithRouteIdentity pins the attribution
// root: the stats handle must be keyed by the route row's own upstream model,
// captured at selection time. Adaptors (aws, baidu_v2, claude, deepseek) rewrite
// RelayInfo.UpstreamModelName mid-flight, so deriving the key later would
// attribute samples to a route unit that was never selected.
func TestSelectRouteUnitAttachesStatsHandleWithRouteIdentity(t *testing.T) {
	const group, alias = "stats-group", "stats-alias"

	ch := testRouteChannel(7001, 10, 5, false, []string{"sk-1"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 7001, 0, group, alias, "upstream-actual", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rand.New(rand.NewPCG(41, 41)))
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.NotNil(t, selected.StatsHandle, "a selected route must carry a stats handle")

	// The handle must be the very same one a lookup by route identity returns.
	want := routestats.GetOrCreateHandle(routestats.RouteKey{
		Group:            group,
		PublicModelAlias: alias,
		ChannelID:        7001,
		KeyIndex:         0,
		UpstreamModel:    "upstream-actual",
	})
	assert.Same(t, want.State(), selected.StatsHandle.State(),
		"handle must be keyed by the route row identity captured at selection time")

	// Recording through the selected route must land on that same state.
	selected.StatsHandle.ObserveSuccess(routestats.SuccessObservation)
	assert.Equal(t, 1, want.Snapshot().SampleCount)
}

// TestSelectedRouteFromChannelHasNoStatsHandle pins the guard for a channel that
// owns no route row for the alias: there is no route unit to charge, so recording
// must stay a no-op rather than inventing an identity from the alias.
func TestSelectedRouteFromChannelHasNoStatsHandle(t *testing.T) {
	ch := testRouteChannel(7002, 10, 5, false, []string{"sk-1"}, nil)

	route, err := SelectedRouteFromChannel(ch, "some-alias")
	require.NoError(t, err)
	require.NotNil(t, route)

	assert.Nil(t, route.StatsHandle, "locked-channel replay must leave recording as a no-op")
	assert.Equal(t, 0, route.RouteId)
}

// TestSelectedRouteFromChannelAttributesRealRoute covers the affinity and
// specific-channel paths. They bypass weighted random selection but still serve a
// real route unit, so their samples must land on that unit -- keyed by the route
// row's upstream model, not by the requested alias.
func TestSelectedRouteFromChannelAttributesRealRoute(t *testing.T) {
	const group, alias = "affinity-group", "affinity-alias"

	ch := testRouteChannel(7003, 10, 5, false, []string{"sk-1"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 7003, 0, group, alias, "upstream-affinity", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()

	route, err := SelectedRouteFromChannel(ch, alias)
	require.NoError(t, err)
	require.NotNil(t, route)

	assert.Equal(t, group, route.Group)
	assert.Equal(t, "upstream-affinity", route.UpstreamModel,
		"upstream must come from the route row, not from the requested alias")
	require.NotNil(t, route.StatsHandle, "a channel that owns a route row must be attributable")

	want := routestats.GetOrCreateHandle(routestats.RouteKey{
		Group:            group,
		PublicModelAlias: alias,
		ChannelID:        7003,
		KeyIndex:         0,
		UpstreamModel:    "upstream-affinity",
	})
	assert.Same(t, want.State(), route.StatsHandle.State())
}
