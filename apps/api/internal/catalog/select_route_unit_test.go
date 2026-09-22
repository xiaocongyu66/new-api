package channel

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func withRouteUnitFixture(t *testing.T, channels []*Channel, group, alias string, routes []ChannelModelRoute) func() {
	t.Helper()

	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := alias2routes
	prevMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
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

	// Build alias2routes from provided routes
	alias2routes = make(map[string][]routeCandidate)
	for _, r := range routes {
		alias2routes[r.PublicModelAlias] = append(
			alias2routes[r.PublicModelAlias],
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
		alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	}
}

// testRouteChannel builds a channel for selector tests. Scheduling weight lives on
// the route unit (see testRoute), not on the channel.
func testRouteChannel(id int, isMultiKey bool, keys []string, keyStatus map[int]int) *Channel {
	ch := &Channel{
		Id:     id,
		Status: common.ChannelStatusEnabled,
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

func testRoute(routeId, channelId, keyIndex int, alias, upstreamModel string, weight int) ChannelModelRoute {
	return ChannelModelRoute{
		Id:               routeId,
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

	ch := testRouteChannel(1001, false, []string{"sk-single"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1001, 0, alias, "upstream-model", 100),
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

	ch := testRouteChannel(1002, true, []string{"sk-key0", "sk-key1", "sk-key2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1002, 0, alias, "upstream-a", 100),
		testRoute(2, 1002, 1, alias, "upstream-b", 100),
		testRoute(3, 1002, 2, alias, "upstream-c", 100),
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
	ch := testRouteChannel(1003, true, []string{"sk-key0", "sk-key1", "sk-key2"}, keyStatus)
	routes := []ChannelModelRoute{
		testRoute(1, 1003, 0, alias, "upstream-a", 100),
		testRoute(2, 1003, 1, alias, "upstream-b", 100),
		testRoute(3, 1003, 2, alias, "upstream-c", 100),
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

	ch1 := testRouteChannel(1004, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1005, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1004, 0, alias, "upstream-1", 100),
		testRoute(2, 1005, 0, alias, "upstream-2", 100),
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

	ch1 := testRouteChannel(1006, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1007, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1006, 0, alias, "upstream-1", 100),
		{Id: 2, PublicModelAlias: alias, ChannelId: 1007, KeyIndex: 0, UpstreamModel: "upstream-2", StaticWeight: 100, Enabled: false}, // disabled
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

	ch1 := testRouteChannel(1008, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(1009, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 1008, 0, alias, "upstream-1", 100),
		testRoute(2, 1009, 0, alias, "upstream-2", 100),
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
	// Thin pools route by power-of-two-choices: the calm route (multiplier 0.5)
	// loses every contest against the healthy route and is served only when
	// both draws land on it (25%), so the healthy route takes ~150 of 200.
	assert.Greater(t, counts[1009], counts[1008], "healthy route must dominate calm route")
	assert.InDelta(t, 150, counts[1009], 25, "healthy route takes 75%% under P2C")

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
	ch1 := testRouteChannel(1010, false, []string{"sk-1"}, nil)
	ch1.Type = constant.ChannelTypeAdvancedCustom
	ch1.OtherSettings = `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","models":["test-model"]}]}}`

	ch2 := testRouteChannel(1011, false, []string{"sk-2"}, nil)
	ch2.Type = constant.ChannelTypeAdvancedCustom
	ch2.OtherSettings = `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/embeddings","models":["test-model"]}]}}`

	routes := []ChannelModelRoute{
		testRoute(1, 1010, 0, alias, "upstream-1", 100),
		testRoute(2, 1011, 0, alias, "upstream-2", 100),
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

	ch1 := testRouteChannel(2001, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(2002, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 2001, 0, alias, "upstream-1", 100),
		testRoute(2, 2002, 0, alias, "upstream-2", 200), // higher weight
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

	ch1 := testRouteChannel(3001, false, []string{"sk-1"}, nil)
	ch2 := testRouteChannel(3002, false, []string{"sk-2"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 3001, 0, alias, "upstream-1", 100), // weight 100
		testRoute(2, 3002, 0, alias, "upstream-2", 300), // weight 300 (3x)
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

	// ch2 has 3x weight. Thin pools draw P2C: a candidate wins its double draws
	// outright (1/4) and takes its base-share slice of the mixed pairs, so the
	// split compresses toward even: P(3002) = 1/4 + 1/2 x 301/402 ≈ 62.4%.
	assert.InDelta(t, 624, counts[3002], 60, "weight 300 compresses to ~62% under P2C")
	assert.InDelta(t, 376, counts[3001], 60, "weight 100 correspondingly ~38%")
}

func TestSelectRouteUnit_NormalizedAliasFallback(t *testing.T) {
	const group = "test-group"
	const alias = "gemini-2.5-flash-thinking-512" // FormatMatchingModelName normalizes this

	const normalizedAlias = "gemini-2.5-flash-thinking-*"
	ch := testRouteChannel(4001, false, []string{"sk-1"}, nil)
	// Use fixture but with normalized alias for both group2model2channels and routes
	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := alias2routes
	prevMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	})

	ids := []int{4001}
	idm := map[int]*Channel{4001: ch}
	channelSyncLock.Lock()
	// Eligibility and route rows are both derived from channel.Models, so they key
	// on the same string — the normalized alias the operator configured.
	group2model2channels = map[string]map[string][]int{group: {normalizedAlias: ids}}
	channelsIDM = idm
	if alias2routes == nil {
		alias2routes = make(map[string][]routeCandidate)
	}
	alias2routes[normalizedAlias] = []routeCandidate{
		{routeId: 1, channelId: 4001, keyIndex: 0, upstreamModel: "upstream-normalized", staticWeight: 100},
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

	ch := testRouteChannel(7001, false, []string{"sk-1"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 7001, 0, alias, "upstream-actual", 100),
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
	ch := testRouteChannel(7002, false, []string{"sk-1"}, nil)

	route, err := SelectedRouteFromChannel(ch, "some-alias", "default")
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

	ch := testRouteChannel(7003, false, []string{"sk-1"}, nil)
	routes := []ChannelModelRoute{
		testRoute(1, 7003, 0, alias, "upstream-affinity", 100),
	}
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, routes)
	defer cleanup()

	route, err := SelectedRouteFromChannel(ch, alias, group)
	require.NoError(t, err)
	require.NotNil(t, route)

	assert.Equal(t, group, route.Group)
	assert.Equal(t, "upstream-affinity", route.UpstreamModel,
		"upstream must come from the route row, not from the requested alias")
	require.NotNil(t, route.StatsHandle, "a channel that owns a route row must be attributable")

	want := routestats.GetOrCreateHandle(routestats.RouteKey{
		PublicModelAlias: alias,
		ChannelID:        7003,
		KeyIndex:         0,
		UpstreamModel:    "upstream-affinity",
	})
	assert.Same(t, want.State(), route.StatsHandle.State())
}

// TestSelectedRouteForProbeKeepsShareWindowClean draws the line between traffic
// and administration. Affinity and locked replay serve real requests, so they
// belong in the share window; a channel test or key probe is the operator poking
// the upstream, and counting it would let one "test all channels" click move the
// window and have the correction chase load no user generated. Both variants must
// still attribute EWMA samples, because a probe's latency and failures are real
// signal about the route.
func TestSelectedRouteForProbeKeepsShareWindowClean(t *testing.T) {
	const group, alias = "probe-group", "probe-alias"

	withRouteStats(t, nil)
	ch := testRouteChannel(7004, false, []string{"sk-1"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, []ChannelModelRoute{
		testRoute(1, 7004, 0, alias, "upstream-probe", 100),
	})
	defer cleanup()

	pool := routestats.PoolKey{PublicModelAlias: alias}
	id := routestats.RouteID{ChannelID: 7004, KeyIndex: 0, UpstreamModel: "upstream-probe"}
	targets := map[routestats.RouteID]float64{id: 1.0}
	cfg := routestats.GetRouteStatsSetting()

	probe, err := SelectedRouteForProbe(ch, alias, group)
	require.NoError(t, err)
	require.NotNil(t, probe.StatsHandle, "a probe still produces EWMA signal for the route")
	assert.Zero(t, routestats.Corrections(pool, targets, cfg)[id].Opportunities,
		"a probe must not appear in the share window")

	served, err := SelectedRouteFromChannel(ch, alias, group)
	require.NoError(t, err)
	require.NotNil(t, served.StatsHandle)
	assert.Equal(t, 1, routestats.Corrections(pool, targets, cfg)[id].Opportunities,
		"real traffic on the same path must be recorded")
	assert.Equal(t, 1, routestats.Corrections(pool, targets, cfg)[id].Selections)
}

// TestSelectedRouteFromChannelBuildsFullRouteForLockedReplay covers the locked-channel
// replay path in its multi-key shape, which is where a partially-built route is
// most damaging.
//
// A locked replay does not run a weighted draw — the channel is already decided —
// but the relay loop consumes the result exactly like a selected route: it reads
// Channel, Key, KeyIndex and UpstreamModel to build the upstream request, and
// StatsHandle to attribute the outcome. So "no draw" must not mean "no identity":
// the returned SelectedRoute has to be complete and consistent, with the key
// index it actually picked matching the route row whose upstream model it
// reports. Deriving upstream from the requested alias instead would charge a
// route unit that never served, and returning a zero RouteId would silently drop
// the replay out of attribution entirely.
func TestSelectedRouteFromChannelBuildsFullRouteForLockedReplay(t *testing.T) {
	const group, alias = "locked-group", "locked-alias"

	// Key 0 is disabled, so the only usable unit is key 1 — the route row whose
	// upstream model differs from both the alias and key 0's upstream.
	ch := testRouteChannel(7101, true, []string{"sk-key0", "sk-key1"},
		map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusEnabled})
	ch.ChannelInfo.MultiKeySize = 2
	ch.ChannelInfo.MultiKeyMode = constant.MultiKeyModeRandom

	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, []ChannelModelRoute{
		testRoute(11, 7101, 0, alias, "upstream-key0", 100),
		testRoute(12, 7101, 1, alias, "upstream-key1", 100),
	})
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	route, err := SelectedRouteFromChannel(ch, alias, group)
	require.NoError(t, err)
	require.NotNil(t, route)

	// Every field the relay loop reads must be populated and mutually consistent.
	assert.Equal(t, 12, route.RouteId, "the replay must name the route row it served")
	assert.Equal(t, group, route.Group, "group comes from the route row, not from the request")
	assert.Equal(t, alias, route.Alias)
	assert.Same(t, ch, route.Channel)
	assert.Equal(t, 7101, route.ChannelId)
	assert.Equal(t, 1, route.KeyIndex, "the disabled key index must not be replayed")
	assert.Equal(t, `"sk-key1"`, route.Key, "the key must be the one at the selected index")
	assert.Equal(t, "upstream-key1", route.UpstreamModel,
		"upstream must come from the selected key's route row, not the alias or a sibling key")
	require.NotNil(t, route.StatsHandle, "a replay serving a real route unit must be attributable")

	// The handle must be the one keyed by that exact route identity, so a sample
	// recorded through the replay lands on the unit that served it.
	want := routestats.GetOrCreateHandle(routestats.RouteKey{
		PublicModelAlias: alias,
		ChannelID:        7101,
		KeyIndex:         1,
		UpstreamModel:    "upstream-key1",
	})
	assert.Same(t, want.State(), route.StatsHandle.State())

	// And it must not be the sibling key's handle, which is the mistake a
	// channel-keyed identity would make.
	sibling := routestats.GetOrCreateHandle(routestats.RouteKey{
		PublicModelAlias: alias,
		ChannelID:        7101,
		KeyIndex:         0,
		UpstreamModel:    "upstream-key0",
	})
	route.StatsHandle.ObserveSuccess(routestats.SuccessObservation)
	assert.Equal(t, 1, want.Snapshot().SampleCount)
	assert.Zero(t, sibling.Snapshot().SampleCount,
		"the sibling route unit on the same channel must be untouched")
}

// driveToDormant escalates one route seven levels so isolationDuration lands it
// in the dormant state (multiplier DormantWeightScale).
func driveToDormant(t *testing.T, key RouteKey) {
	t.Helper()
	now := time.Now()
	for range 7 {
		require.NoError(t, RecordRetryableFailure(key, "bad_response", FailureSourceUpstream, now))
	}
	require.Equal(t, HealthDormant, getHealthState(t, key), "fixture must reach dormant")
}

func getHealthState(t *testing.T, key RouteKey) string {
	t.Helper()
	state, _, _, ok := GetRouteIsolation(key)
	require.True(t, ok, "route %v must have a health row", key)
	return state
}

// TestThinPoolP2CStateBeatsWeight pins the core P2C rule: in a thin pool a
// healthier isolation state wins the sampled pair regardless of static weight.
// A dormant route with 10x the healthy route's weight still loses every mixed
// draw, so the healthy route serves ~75% (the dormant route is probed only
// when both uniform draws land on it: 1/4).
func TestThinPoolP2CStateBeatsWeight(t *testing.T) {
	const group, alias = "p2c-group", "p2c-model"

	light := testRouteChannel(8301, false, []string{"sk-light"}, nil)
	heavy := testRouteChannel(8302, false, []string{"sk-heavy"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{light, heavy}, group, alias, []ChannelModelRoute{
		testRoute(1, 8301, 0, alias, "up-light", 1),
		testRoute(2, 8302, 0, alias, "up-heavy", 10),
	})
	defer cleanup()

	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	driveToDormant(t, RouteKey{ChannelId: 8302, KeyIndex: 0, Model: alias})

	counts := drawShares(t, group, alias, 2000, 0xA2C01)
	assert.InDelta(t, 1500, counts[8301], 120,
		"the healthy route must win every mixed pair (~75%%), got %d", counts[8301])
	assert.Positive(t, counts[8302], "the dormant route keeps its 1/n² probe share")
}

// TestThinPoolP2CThreeRouteProbeShare pins the 1/n² probe share: with three
// equal-weight candidates and one dormant, the dormant route wins only the
// double draws (1/9 ≈ 11%), while its two healthy peers split the rest evenly.
func TestThinPoolP2CThreeRouteProbeShare(t *testing.T) {
	const group, alias = "p2c3-group", "p2c3-model"

	chA := testRouteChannel(8311, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(8312, false, []string{"sk-b"}, nil)
	chC := testRouteChannel(8313, false, []string{"sk-c"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB, chC}, group, alias, []ChannelModelRoute{
		testRoute(1, 8311, 0, alias, "up-a", 100),
		testRoute(2, 8312, 0, alias, "up-b", 100),
		testRoute(3, 8313, 0, alias, "up-c", 100),
	})
	defer cleanup()

	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	driveToDormant(t, RouteKey{ChannelId: 8313, KeyIndex: 0, Model: alias})

	counts := drawShares(t, group, alias, 3000, 0xA2C02)
	assert.InDelta(t, 333, counts[8313], 90,
		"one dormant route in a three-route pool is probed ~1/9 of the time, got %d", counts[8313])
	assert.Greater(t, counts[8311], counts[8313], "a healthy peer must dominate the dormant route")
	assert.Greater(t, counts[8312], counts[8313], "both healthy peers must dominate the dormant route")
}

// TestThinPoolP2CDeterministicUnderSeed pins that P2C consumes randomness only
// through the injected source: identical seeds must produce identical selection
// sequences, so distribution tests stay reproducible.
func TestThinPoolP2CDeterministicUnderSeed(t *testing.T) {
	const group, alias = "p2cd-group", "p2cd-model"

	chA := testRouteChannel(8321, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(8322, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 8321, 0, alias, "up-a", 100),
		testRoute(2, 8322, 0, alias, "up-b", 40),
	})
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	run := func() []int {
		rnd := rand.New(rand.NewPCG(0x51EE, 7))
		ids := make([]int, 0, 50)
		for range 50 {
			selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
			require.NoError(t, err)
			require.NotNil(t, selected)
			ids = append(ids, selected.ChannelId)
		}
		return ids
	}
	assert.Equal(t, run(), run(), "same seed must reproduce the same P2C sequence")
}

// TestThinPoolP2CFourCandidatesUniform pins the P2C boundary: four equal
// candidates (< smallPoolUnits=5) still route by P2C, and with equal health
// and weight each lands on exactly 1/4 of the traffic.
func TestThinPoolP2CFourCandidatesUniform(t *testing.T) {
	const group, alias = "p2c4-group", "p2c4-model"

	chA := testRouteChannel(8331, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(8332, false, []string{"sk-b"}, nil)
	chC := testRouteChannel(8333, false, []string{"sk-c"}, nil)
	chD := testRouteChannel(8334, false, []string{"sk-d"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB, chC, chD}, group, alias, []ChannelModelRoute{
		testRoute(1, 8331, 0, alias, "up-a", 100),
		testRoute(2, 8332, 0, alias, "up-b", 100),
		testRoute(3, 8333, 0, alias, "up-c", 100),
		testRoute(4, 8334, 0, alias, "up-d", 100),
	})
	defer cleanup()

	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	counts := drawShares(t, group, alias, 4000, 0xA2C03)
	for _, id := range []int{8331, 8332, 8333, 8334} {
		assert.InDelta(t, 1000, counts[id], 120,
			"four equal candidates must each take 1/4 under P2C, got %d for %d", counts[id], id)
	}
}
