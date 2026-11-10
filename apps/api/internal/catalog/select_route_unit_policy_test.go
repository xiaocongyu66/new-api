package channel

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// withDispatchFixture installs the memory-cache state the dispatch-policy
// selector reads: the channel index, the group eligibility set, the
// alias->route-unit index, the dispatch-policy snapshot and the context-route
// snapshot. It follows withRouteUnitFixture's save/restore shape and
// additionally covers dispatchPolicies / contextRoutesByAlias, which
// withRouteUnitFixture does not touch. Weighted-pool aliases are seeded
// through routes ([]ChannelModelRoute); context_range aliases through
// contextRoutes (keyed by alias, exactly like the production snapshot).
func withDispatchFixture(t *testing.T, channels []*Channel, group string, aliases []string, policies map[string]*ModelAliasDispatchPolicy, contextRoutes map[string][]ModelAliasContextRoute, routes []ChannelModelRoute) func() {
	t.Helper()

	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := alias2routes
	prevPolicies := dispatchPolicies
	prevContextRoutes := contextRoutesByAlias
	prevMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
		dispatchPolicies = prevPolicies
		contextRoutesByAlias = prevContextRoutes
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
	eligibility := make(map[string][]int, len(aliases))
	for _, alias := range aliases {
		eligibility[alias] = ids
	}
	group2model2channels = map[string]map[string][]int{group: eligibility}
	channelsIDM = idm
	channel2advancedCustomConfig = advancedCustomConfig

	// Build alias2routes from provided routes (weighted-pool aliases)
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
	dispatchPolicies = policies
	contextRoutesByAlias = contextRoutes
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	return func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
		dispatchPolicies = prevPolicies
		contextRoutesByAlias = prevContextRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	}
}

// testDispatchPolicy builds a policy for the dispatcher fixtures. An empty
// fallback leaves FallbackAlias nil, which is how an operator expresses
// "no fallback configured".
func testDispatchPolicy(alias, mode, fallback string) *ModelAliasDispatchPolicy {
	policy := &ModelAliasDispatchPolicy{Alias: alias, Mode: mode}
	if fallback != "" {
		policy.FallbackAlias = &fallback
	}
	return policy
}

// testContextRoute builds an enabled context-route row for the dispatcher
// fixtures, mirroring testRoute for the weighted-pool index. KeyIndex is
// always 0: the tests below exercise single-key channels.
func testContextRoute(id int, alias string, contextMin, contextMax, channelId int, upstream string, weight, sortOrder int) ModelAliasContextRoute {
	return ModelAliasContextRoute{
		Id:            id,
		Alias:         alias,
		ContextMin:    contextMin,
		ContextMax:    contextMax,
		ChannelId:     channelId,
		KeyIndex:      0,
		UpstreamModel: upstream,
		StaticWeight:  weight,
		Enabled:       true,
		SortOrder:     sortOrder,
	}
}

// TestContextRangeIntervalMatchingBoundaries pins the interval rule of
// context-range matching: a row matches when ContextMin <= inputTokens <=
// ContextMax, with ContextMax == 0 meaning unbounded. An interval with
// min > max can never satisfy both halves of the condition, so it is dead
// configuration — and zero tokens can never enter an interval with min > 0.
func TestContextRangeIntervalMatchingBoundaries(t *testing.T) {
	const group = "ctx-boundary-group"
	const aliasBounded = "ctx-bounded"
	const aliasInverted = "ctx-inverted"

	chLow := testRouteChannel(3101, false, []string{"sk-low"}, nil)
	chHuge := testRouteChannel(3102, false, []string{"sk-huge"}, nil)
	chInverted := testRouteChannel(3103, false, []string{"sk-inverted"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chLow, chHuge, chInverted}, group,
		[]string{aliasBounded, aliasInverted},
		map[string]*ModelAliasDispatchPolicy{
			aliasBounded:  testDispatchPolicy(aliasBounded, "context_range", ""),
			aliasInverted: testDispatchPolicy(aliasInverted, "context_range", ""),
		},
		map[string][]ModelAliasContextRoute{
			aliasBounded: {
				testContextRoute(1, aliasBounded, 100, 200, 3101, "up-short", 100, 0),
				testContextRoute(2, aliasBounded, 5000, 0, 3102, "up-long", 100, 1),
			},
			aliasInverted: {
				testContextRoute(3, aliasInverted, 500, 100, 3103, "up-inverted", 100, 0),
			},
		},
		nil,
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	tests := []struct {
		name        string
		alias       string
		inputTokens int
		wantChannel int // 0 = nothing may be selected
	}{
		{name: "input equals context_min matches", alias: aliasBounded, inputTokens: 100, wantChannel: 3101},
		{name: "input inside the interval matches", alias: aliasBounded, inputTokens: 150, wantChannel: 3101},
		{name: "input equals context_max matches", alias: aliasBounded, inputTokens: 200, wantChannel: 3101},
		{name: "context_max 0 is unbounded", alias: aliasBounded, inputTokens: 2_000_000, wantChannel: 3102},
		{name: "zero tokens never enter a min>0 interval", alias: aliasBounded, inputTokens: 0, wantChannel: 0},
		{name: "gap between intervals does not match", alias: aliasBounded, inputTokens: 201, wantChannel: 0},
		{name: "min>max interval rejects a token below min", alias: aliasInverted, inputTokens: 50, wantChannel: 0},
		{name: "min>max interval rejects a token above max", alias: aliasInverted, inputTokens: 500, wantChannel: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rnd := rand.New(rand.NewPCG(42, uint64(tt.inputTokens)))
			selected, err := SelectRouteUnit(group, tt.alias, "", 0, nil, tt.inputTokens, rnd)
			require.NoError(t, err)
			if tt.wantChannel == 0 {
				assert.Nil(t, selected, "no interval contains these tokens and no fallback is configured")
				return
			}
			require.NotNil(t, selected)
			assert.Equal(t, tt.wantChannel, selected.ChannelId)
		})
	}
}

// TestContextRangeRoutesOnlyWithinMatchingInterval pins the interval as the
// dispatch boundary: across repeated seeded draws, tokens inside one interval
// may only ever land on the channels that interval's rows point at.
func TestContextRangeRoutesOnlyWithinMatchingInterval(t *testing.T) {
	const group, alias = "ctx-scope-group", "ctx-scope"

	chShortA := testRouteChannel(3201, false, []string{"sk-short-a"}, nil)
	chShortB := testRouteChannel(3202, false, []string{"sk-short-b"}, nil)
	chLong := testRouteChannel(3203, false, []string{"sk-long"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chShortA, chShortB, chLong}, group, []string{alias},
		map[string]*ModelAliasDispatchPolicy{
			alias: testDispatchPolicy(alias, "context_range", ""),
		},
		map[string][]ModelAliasContextRoute{
			alias: {
				testContextRoute(1, alias, 1, 1000, 3201, "up-short-a", 100, 0),
				testContextRoute(2, alias, 1, 1000, 3202, "up-short-b", 100, 1),
				testContextRoute(3, alias, 1001, 2000, 3203, "up-long", 100, 2),
			},
		},
		nil,
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	rnd := rand.New(rand.NewPCG(0xC0FFEE, 7))
	shortChannels := make(map[int]int)
	for range 150 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, 500, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		shortChannels[selected.ChannelId]++
	}
	require.Len(t, shortChannels, 2, "both channels of the matching interval must serve traffic")
	assert.Positive(t, shortChannels[3201])
	assert.Positive(t, shortChannels[3202])

	longChannels := make(map[int]int)
	for range 100 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, 1500, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		longChannels[selected.ChannelId]++
	}
	require.Len(t, longChannels, 1)
	assert.Equal(t, 100, longChannels[3203], "the second interval routes exclusively to its own channel")
}

// TestContextRangeFallsBackWhenNoIntervalMatches pins the no-match fallback:
// tokens outside every interval of a context_range alias, with no (0,0)
// default row, resolve through the fallback alias's weighted pool. The
// request never fails and never lands on a context row of the alias itself.
func TestContextRangeFallsBackWhenNoIntervalMatches(t *testing.T) {
	const group, aliasA, aliasB = "ctx-fallback-group", "ctx-fallback-a", "ctx-fallback-b"

	chA1 := testRouteChannel(3301, false, []string{"sk-a1"}, nil)
	chA2 := testRouteChannel(3302, false, []string{"sk-a2"}, nil)
	chB1 := testRouteChannel(3311, false, []string{"sk-b1"}, nil)
	chB2 := testRouteChannel(3312, false, []string{"sk-b2"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chA1, chA2, chB1, chB2}, group, []string{aliasA, aliasB},
		map[string]*ModelAliasDispatchPolicy{
			aliasA: testDispatchPolicy(aliasA, "context_range", aliasB),
		},
		map[string][]ModelAliasContextRoute{
			aliasA: {
				testContextRoute(1, aliasA, 1, 100, 3301, "up-a-short", 100, 0),
				testContextRoute(2, aliasA, 5000, 9999, 3302, "up-a-long", 100, 1),
			},
		},
		[]ChannelModelRoute{
			testRoute(10, 3311, 0, aliasB, "up-b-one", 100),
			testRoute(11, 3312, 0, aliasB, "up-b-two", 100),
		},
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	rnd := rand.New(rand.NewPCG(0xF0B, 3))
	counts := make(map[int]int)
	for range 100 {
		// 250 sits in the gap between A's two intervals.
		selected, err := SelectRouteUnit(group, aliasA, "", 0, nil, 250, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected, "tokens outside every interval must reach the fallback pool, not fail")
		counts[selected.ChannelId]++
		assert.Equal(t, aliasB, selected.Alias, "the selection resolved under the fallback alias")
	}
	assert.Zero(t, counts[3301], "A's context rows never serve on the fallback path")
	assert.Zero(t, counts[3302], "A's context rows never serve on the fallback path")
	assert.Positive(t, counts[3311])
	assert.Positive(t, counts[3312])
}

// TestContextRangeFallbackResolvesFromDBWhenCacheOff pins the same fallback on
// the cache-off path: with MEMORY_CACHE_ENABLED off the policy, the context
// rows and the fallback alias's weighted pool all come from per-request
// database reads, and the snapshot globals must not be consulted.
func TestContextRangeFallbackResolvesFromDBWhenCacheOff(t *testing.T) {
	const group, aliasA, aliasB = "ctx-off-group", "ctx-off-a", "ctx-off-b"

	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	require.NoError(t, dbx.DB.AutoMigrate(&ModelAliasDispatchPolicy{}, &ModelAliasContextRoute{}))

	seedGroupIsolationChannel(t, dbx.DB, 3901, []string{group}, aliasB)

	fallback := aliasB
	require.NoError(t, dbx.DB.Create(&ModelAliasDispatchPolicy{
		Alias: aliasA, Mode: "context_range", FallbackAlias: &fallback,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: aliasA, ContextMin: 1, ContextMax: 100, ChannelId: 3999,
		UpstreamModel: "up-a", StaticWeight: 100, Enabled: true,
	}).Error)

	prevMemoryCache := common.MemoryCacheEnabled
	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := alias2routes
	prevPolicies := dispatchPolicies
	prevContextRoutes := contextRoutesByAlias
	common.MemoryCacheEnabled = false
	// Leave every index unbuilt: this is the real cache-off state
	// InitChannelCache produces (it early-returns before assigning them).
	channelSyncLock.Lock()
	channelsIDM = nil
	group2model2channels = nil
	alias2routes = nil
	dispatchPolicies = nil
	contextRoutesByAlias = nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
		dispatchPolicies = prevPolicies
		contextRoutesByAlias = prevContextRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
	})

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	// 500 sits outside A's only interval, so the selection must resolve the
	// fallback alias entirely from the database.
	selected, err := SelectRouteUnit(group, aliasA, "", 0, nil, 500, rand.New(rand.NewPCG(0xDB, 1)))
	require.NoError(t, err)
	require.NotNil(t, selected, "cache-off fallback must resolve the fallback alias from the database")
	assert.Equal(t, 3901, selected.ChannelId)
	assert.Equal(t, aliasB, selected.Alias)
	assert.Equal(t, aliasB, selected.UpstreamModel, "the upstream model comes from B's route row")
	assert.Equal(t, "sk-iso", selected.Key)
}

// TestContextRangeFallsBackWhenIntervalPoolIsDown pins the second fallback
// trigger: the interval matched, but every channel inside it is terminal-
// disabled, so selectP2C has no eligible candidate (faint recall refuses
// hard-disabled units) and the fallback alias serves the request instead of
// a hard failure.
func TestContextRangeFallsBackWhenIntervalPoolIsDown(t *testing.T) {
	const group, aliasA, aliasB = "ctx-down-group", "ctx-down-a", "ctx-down-b"

	chA := testRouteChannel(3401, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(3411, false, []string{"sk-b"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chA, chB}, group, []string{aliasA, aliasB},
		map[string]*ModelAliasDispatchPolicy{
			aliasA: testDispatchPolicy(aliasA, "context_range", aliasB),
		},
		map[string][]ModelAliasContextRoute{
			aliasA: {testContextRoute(1, aliasA, 1, 100, 3401, "up-a", 100, 0)},
		},
		[]ChannelModelRoute{testRoute(10, 3411, 0, aliasB, "up-b", 100)},
	)
	defer cleanup()

	withUnitHealthDB(t)
	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	// Healthy first: the matching interval serves its own channel.
	rnd := rand.New(rand.NewPCG(0xD00, 9))
	selected, err := SelectRouteUnit(group, aliasA, "", 0, nil, 50, rnd)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 3401, selected.ChannelId)

	// Terminal-disable the interval's only unit.
	now := time.Now()
	removed := RouteKey{ChannelId: 3401, KeyIndex: 0, Model: aliasA}
	require.NoError(t, DisableUnit(removed, now))
	assert.False(t, IsUnitSelectable(removed, now))

	for range 20 {
		selected, err := SelectRouteUnit(group, aliasA, "", 0, nil, 50, rand.New(rand.NewPCG(0xD11, 11)))
		require.NoError(t, err)
		require.NotNil(t, selected, "a fully down interval must fall back instead of hard-failing")
		assert.Equal(t, 3411, selected.ChannelId, "the fallback alias's pool serves the request")
		assert.Equal(t, aliasB, selected.Alias)
	}
}

// TestContextRangeFallbackCyclesTerminate pins the visited-set guarantee: a
// misconfigured fallback cycle (A->B->A, and the three-hop A->B(delegate)->
// C->A) must terminate and yield no route — never spin forever.
func TestContextRangeFallbackCyclesTerminate(t *testing.T) {
	const group = "ctx-cycle-group"
	const aliasA1, aliasB1 = "ctx-cycle-a1", "ctx-cycle-b1"
	const aliasA2, aliasB2, aliasC2 = "ctx-cycle-a2", "ctx-cycle-b2", "ctx-cycle-c2"

	chA1 := testRouteChannel(3501, false, []string{"sk-a1"}, nil)
	chB1 := testRouteChannel(3502, false, []string{"sk-b1"}, nil)
	chA2 := testRouteChannel(3511, false, []string{"sk-a2"}, nil)
	chC2 := testRouteChannel(3512, false, []string{"sk-c2"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chA1, chB1, chA2, chC2}, group,
		[]string{aliasA1, aliasB1, aliasA2, aliasB2, aliasC2},
		map[string]*ModelAliasDispatchPolicy{
			// Two-cycle: A1 falls back to B1, B1 falls back to A1.
			aliasA1: testDispatchPolicy(aliasA1, "context_range", aliasB1),
			aliasB1: testDispatchPolicy(aliasB1, "context_range", aliasA1),
			// Three-cycle: A2 -> B2 (delegate) -> C2 -> A2.
			aliasA2: testDispatchPolicy(aliasA2, "context_range", aliasB2),
			aliasB2: testDispatchPolicy(aliasB2, "delegate", aliasC2),
			aliasC2: testDispatchPolicy(aliasC2, "context_range", aliasA2),
		},
		map[string][]ModelAliasContextRoute{
			aliasA1: {testContextRoute(1, aliasA1, 1, 100, 3501, "up-a1", 100, 0)},
			aliasB1: {testContextRoute(2, aliasB1, 1, 100, 3502, "up-b1", 100, 0)},
			aliasA2: {testContextRoute(3, aliasA2, 1, 100, 3511, "up-a2", 100, 0)},
			aliasC2: {testContextRoute(4, aliasC2, 1, 100, 3512, "up-c2", 100, 0)},
		},
		nil,
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	// Tokens 500 match no interval of any alias in either cycle, so every
	// hop takes its fallback edge until the visited set cuts the walk.
	rnd := rand.New(rand.NewPCG(0xC7C1E, 5))

	t.Run("two alias cycle terminates with no route", func(t *testing.T) {
		selected, err := SelectRouteUnit(group, aliasA1, "", 0, nil, 500, rnd)
		require.NoError(t, err)
		assert.Nil(t, selected, "A->B->A must terminate via the visited set and yield nothing")
	})

	t.Run("three alias cycle terminates with no route", func(t *testing.T) {
		selected, err := SelectRouteUnit(group, aliasA2, "", 0, nil, 500, rnd)
		require.NoError(t, err)
		assert.Nil(t, selected, "A->B(delegate)->C->A must terminate via the visited set and yield nothing")
	})
}

// TestDelegateModeResolvesThroughFallback pins delegate dispatch: an alias in
// delegate mode never draws from its own weighted pool while a fallback alias
// is configured — the fallback's pool serves every request. With no fallback
// configured, delegate degrades to the alias's own weighted pool.
func TestDelegateModeResolvesThroughFallback(t *testing.T) {
	const group = "delegate-group"
	const aliasDelegating, aliasDelegate, aliasSelfDelegating = "delegate-a", "delegate-b", "delegate-solo"

	chOwn := testRouteChannel(3601, false, []string{"sk-own"}, nil)
	chDelegate := testRouteChannel(3611, false, []string{"sk-delegate"}, nil)
	chSolo := testRouteChannel(3621, false, []string{"sk-solo"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chOwn, chDelegate, chSolo}, group,
		[]string{aliasDelegating, aliasDelegate, aliasSelfDelegating},
		map[string]*ModelAliasDispatchPolicy{
			aliasDelegating:     testDispatchPolicy(aliasDelegating, "delegate", aliasDelegate),
			aliasSelfDelegating: testDispatchPolicy(aliasSelfDelegating, "delegate", ""),
		},
		nil,
		[]ChannelModelRoute{
			testRoute(1, 3601, 0, aliasDelegating, "up-own", 100),
			testRoute(2, 3611, 0, aliasDelegate, "up-delegate", 100),
			testRoute(3, 3621, 0, aliasSelfDelegating, "up-solo", 100),
		},
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	rnd := rand.New(rand.NewPCG(0xDE1, 4))
	for range 20 {
		selected, err := SelectRouteUnit(group, aliasDelegating, "", 0, nil, 0, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, 3611, selected.ChannelId, "delegate mode resolves through the fallback alias's pool")
		assert.Equal(t, aliasDelegate, selected.Alias)
		assert.Equal(t, "up-delegate", selected.UpstreamModel)
	}

	// Delegate without a fallback falls through to the alias's own pool.
	for range 20 {
		selected, err := SelectRouteUnit(group, aliasSelfDelegating, "", 0, nil, 0, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, 3621, selected.ChannelId, "delegate with no fallback serves its own weighted pool")
		assert.Equal(t, aliasSelfDelegating, selected.Alias)
	}
}

// TestRewriteExcludeRoutesForAliasRetargetsModelKeys is the direct unit test of
// the exclude-set rewrite: keys carrying the requested alias's Model are
// retargeted to the fallback alias so a retry cannot immediately re-pick the
// channel/key that just failed, and the input set is left untouched.
func TestRewriteExcludeRoutesForAliasRetargetsModelKeys(t *testing.T) {
	excludes := map[RouteKey]bool{
		{ChannelId: 1, KeyIndex: 0, Model: "origin-alias"}: true,
		{ChannelId: 2, KeyIndex: 1, Model: "other-alias"}:  true,
	}

	rewritten := rewriteExcludeRoutesForAlias(excludes, "fallback-alias")
	require.Len(t, rewritten, 2)
	assert.True(t, rewritten[RouteKey{ChannelId: 1, KeyIndex: 0, Model: "fallback-alias"}],
		"exclusions carried under the requested alias must be retargeted to the fallback alias")
	assert.True(t, rewritten[RouteKey{ChannelId: 2, KeyIndex: 1, Model: "fallback-alias"}],
		"the rewrite applies to the whole set, not just the requesting alias's entries")
	assert.Len(t, excludes, 2, "the input set must not be mutated")
	assert.True(t, excludes[RouteKey{ChannelId: 1, KeyIndex: 0, Model: "origin-alias"}],
		"the caller's original key must survive the rewrite")

	assert.Empty(t, rewriteExcludeRoutesForAlias(nil, "fallback-alias"), "nothing to rewrite")
	assert.Empty(t, rewriteExcludeRoutesForAlias(map[RouteKey]bool{}, "fallback-alias"))
}

// TestContextRangeFallbackHonorsRewrittenExcludes pins the retry-path contract
// end to end: an exclude set built under alias A is honored against the
// fallback alias B's weighted-pool rows. Without the rewrite, B's rows (which
// carry Model=B) would never match the A-keyed exclusion and the failed
// channel would keep winning draws.
func TestContextRangeFallbackHonorsRewrittenExcludes(t *testing.T) {
	const group, aliasA, aliasB = "ctx-exclude-group", "ctx-exclude-a", "ctx-exclude-b"

	chA := testRouteChannel(3701, false, []string{"sk-a"}, nil)
	chB1 := testRouteChannel(3711, false, []string{"sk-b1"}, nil)
	chB2 := testRouteChannel(3712, false, []string{"sk-b2"}, nil)

	cleanup := withDispatchFixture(t,
		[]*Channel{chA, chB1, chB2}, group, []string{aliasA, aliasB},
		map[string]*ModelAliasDispatchPolicy{
			aliasA: testDispatchPolicy(aliasA, "context_range", aliasB),
		},
		map[string][]ModelAliasContextRoute{
			aliasA: {testContextRoute(1, aliasA, 1, 100, 3701, "up-a", 100, 0)},
		},
		[]ChannelModelRoute{
			testRoute(10, 3711, 0, aliasB, "up-b1", 100),
			testRoute(11, 3712, 0, aliasB, "up-b2", 100),
		},
	)
	defer cleanup()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	// The retry excluded a unit under alias A. Tokens 500 match no interval of
	// A, so the selection falls back to B's pool — where the rewritten exclude
	// must keep channel 3711 out, leaving 3712 as the only survivor.
	excludes := map[RouteKey]bool{{ChannelId: 3711, KeyIndex: 0, Model: aliasA}: true}
	rnd := rand.New(rand.NewPCG(0xE0D, 6))
	for range 50 {
		selected, err := SelectRouteUnit(group, aliasA, "", 0, excludes, 500, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, 3712, selected.ChannelId, "the excluded unit must stay excluded after the alias rewrite")
	}
}

// TestSaveModelAliasDispatchPersistsPolicyAndRoutes covers the combined save:
// the policy row and the interval rows land in one transaction, the route rows
// are normalized (Alias forced to the saved alias, Id zeroed so the row is
// auto-incremented), and a second save replaces the alias's rows without
// touching sibling aliases.
func TestSaveModelAliasDispatchPersistsPolicyAndRoutes(t *testing.T) {
	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	require.NoError(t, dbx.DB.AutoMigrate(&ModelAliasDispatchPolicy{}, &ModelAliasContextRoute{}))

	// A row for a sibling alias must survive the replace-delete of the target.
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: "other-alias", ChannelId: 8001, Enabled: true,
	}).Error)

	fallback := "fallback-alias"
	routes := []ModelAliasContextRoute{
		{Id: 77, Alias: "wrong", ContextMin: 1, ContextMax: 100, ChannelId: 8002, UpstreamModel: "up-short", StaticWeight: 30, Enabled: true, SortOrder: 10},
		{Id: 88, Alias: "wrong", ContextMin: 101, ContextMax: 0, ChannelId: 8003, UpstreamModel: "up-long", StaticWeight: 60, Enabled: true, SortOrder: 20},
	}
	require.NoError(t, SaveModelAliasDispatch("ctx-alias", "context_range", &fallback, routes))

	policy, err := GetModelAliasDispatchPolicy("ctx-alias")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "context_range", policy.Mode)
	require.NotNil(t, policy.FallbackAlias)
	assert.Equal(t, "fallback-alias", *policy.FallbackAlias)

	saved, err := ListModelAliasContextRoutes("ctx-alias")
	require.NoError(t, err)
	require.Len(t, saved, 2)
	for i, row := range saved {
		assert.Equal(t, "ctx-alias", row.Alias, "Alias must be forced to the saved alias")
		assert.Positive(t, row.Id, "the zeroed Id must be replaced by an auto-incremented one")
		assert.NotEqual(t, routes[i].Id, row.Id, "the caller's Id must not be persisted")
		assert.Equal(t, routes[i].ContextMin, row.ContextMin)
		assert.Equal(t, routes[i].ContextMax, row.ContextMax)
		assert.Equal(t, routes[i].ChannelId, row.ChannelId)
	}
	assert.Equal(t, 10, saved[0].SortOrder, "rows persist in input order (sort_order ASC, id ASC)")
	assert.Equal(t, 20, saved[1].SortOrder)

	var siblings int64
	require.NoError(t, dbx.DB.Model(&ModelAliasContextRoute{}).Where("alias = ?", "other-alias").Count(&siblings).Error)
	assert.Equal(t, int64(1), siblings, "the replace-delete is scoped to the saved alias")

	// A second save swaps both the policy and the row set in one transaction.
	replacement := []ModelAliasContextRoute{
		{ContextMin: 1, ContextMax: 200, ChannelId: 8002, UpstreamModel: "up-short", StaticWeight: 50, Enabled: true, SortOrder: 5},
	}
	require.NoError(t, SaveModelAliasDispatch("ctx-alias", "weighted_pool", nil, replacement))
	saved, err = ListModelAliasContextRoutes("ctx-alias")
	require.NoError(t, err)
	require.Len(t, saved, 1)
	assert.Equal(t, 5, saved[0].SortOrder)
	policy, err = GetModelAliasDispatchPolicy("ctx-alias")
	require.NoError(t, err)
	assert.Equal(t, "weighted_pool", policy.Mode)
	assert.Nil(t, policy.FallbackAlias, "a cleared fallback must be persisted as NULL")
}

// TestSaveModelAliasDispatchRejectsInvalidInput pins the guard rails: an
// unknown mode and an empty alias are rejected before anything is written, so
// a failed save can never leave a half-updated dispatch configuration behind.
func TestSaveModelAliasDispatchRejectsInvalidInput(t *testing.T) {
	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	require.NoError(t, dbx.DB.AutoMigrate(&ModelAliasDispatchPolicy{}, &ModelAliasContextRoute{}))

	require.NoError(t, SaveModelAliasDispatch("ctx-alias", "weighted_pool", nil, nil))

	tests := []struct {
		name  string
		alias string
		mode  string
	}{
		{name: "unknown mode", alias: "alias-bad", mode: "random_pick"},
		{name: "empty mode", alias: "alias-bad", mode: ""},
		{name: "empty alias", alias: "", mode: "context_range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SaveModelAliasDispatch(tt.alias, tt.mode, nil, nil)
			require.Error(t, err)
		})
	}

	_, err := GetModelAliasDispatchPolicy("alias-bad")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "a rejected save must not create the policy row")

	var routes int64
	require.NoError(t, dbx.DB.Model(&ModelAliasContextRoute{}).Where("alias = ?", "alias-bad").Count(&routes).Error)
	assert.Zero(t, routes, "a rejected save must not create context rows")

	policy, err := GetModelAliasDispatchPolicy("ctx-alias")
	require.NoError(t, err)
	assert.Equal(t, "weighted_pool", policy.Mode, "the pre-existing policy is untouched by rejected saves")
}

// TestSeedModelAliasDispatchPoliciesIsIdempotent pins the startup seed: every
// alias that has route rows gets a weighted_pool policy, an operator-tuned
// mode (context_range) survives untouched, and running the seed again changes
// nothing. The empty alias is skipped.
func TestSeedModelAliasDispatchPoliciesIsIdempotent(t *testing.T) {
	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	require.NoError(t, dbx.DB.AutoMigrate(&ModelAliasDispatchPolicy{}, &ModelAliasContextRoute{}))

	require.NoError(t, dbx.DB.Create(&ChannelModelRoute{
		Id: 1, PublicModelAlias: "seed-a", ChannelId: 8101, KeyIndex: 0,
		UpstreamModel: "up-a", StaticWeight: 100, Enabled: true,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ChannelModelRoute{
		Id: 2, PublicModelAlias: "seed-b", ChannelId: 8102, KeyIndex: 0,
		UpstreamModel: "up-b", StaticWeight: 100, Enabled: true,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ChannelModelRoute{
		Id: 3, PublicModelAlias: "", ChannelId: 8103, KeyIndex: 0,
		UpstreamModel: "up-c", StaticWeight: 100, Enabled: true,
	}).Error)

	// An operator already tuned seed-a; the seed must not overwrite it. The
	// stale UpdatedAt proves the row was not rewritten (an upsert would stamp
	// it with now).
	tunedAt := time.Now().Add(-time.Hour)
	require.NoError(t, dbx.DB.Create(&ModelAliasDispatchPolicy{
		Alias: "seed-a", Mode: "context_range", UpdatedAt: tunedAt,
	}).Error)

	require.NoError(t, SeedModelAliasDispatchPolicies())

	policyA, err := GetModelAliasDispatchPolicy("seed-a")
	require.NoError(t, err)
	assert.Equal(t, "context_range", policyA.Mode, "the tuned mode must survive the seed")
	assert.WithinDuration(t, tunedAt, policyA.UpdatedAt, time.Minute, "the existing row must be untouched")

	policyB, err := GetModelAliasDispatchPolicy("seed-b")
	require.NoError(t, err)
	assert.Equal(t, "weighted_pool", policyB.Mode, "a missing alias gets the default mode")

	var emptyAlias int64
	require.NoError(t, dbx.DB.Model(&ModelAliasDispatchPolicy{}).Where("alias = ?", "").Count(&emptyAlias).Error)
	assert.Zero(t, emptyAlias, "the empty-alias route row must be skipped")

	// Second pass: idempotent, still non-destructive.
	require.NoError(t, SeedModelAliasDispatchPolicies())
	policyA2, err := GetModelAliasDispatchPolicy("seed-a")
	require.NoError(t, err)
	assert.Equal(t, "context_range", policyA2.Mode)
	var total int64
	require.NoError(t, dbx.DB.Model(&ModelAliasDispatchPolicy{}).Count(&total).Error)
	assert.Equal(t, int64(2), total, "repeated seeding must not duplicate policy rows")
}

// TestBuildDispatchCachesFromDBLoadsPoliciesAndEnabledSortedRoutes pins the
// snapshot builder: every policy row lands in the map, and the context-route
// snapshot holds only ENABLED rows sorted by (sort_order, id) — insertion
// order must not leak into dispatch order.
func TestBuildDispatchCachesFromDBLoadsPoliciesAndEnabledSortedRoutes(t *testing.T) {
	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	require.NoError(t, dbx.DB.AutoMigrate(&ModelAliasDispatchPolicy{}, &ModelAliasContextRoute{}))

	fallback := "fallback-alias"
	require.NoError(t, dbx.DB.Create(&ModelAliasDispatchPolicy{
		Alias: "cache-a", Mode: "context_range", FallbackAlias: &fallback,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ModelAliasDispatchPolicy{
		Alias: "cache-b", Mode: "weighted_pool",
	}).Error)

	// sort_order 20 inserted before 10: the snapshot must be sorted by
	// sort_order (then id), not by insertion order. The disabled row at
	// sort_order 1 must not enter the snapshot at all.
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: "cache-a", ContextMin: 1, ContextMax: 100, ChannelId: 8201, Enabled: true, SortOrder: 20,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: "cache-a", ContextMin: 101, ContextMax: 200, ChannelId: 8202, Enabled: true, SortOrder: 10,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: "cache-a", ContextMin: 201, ContextMax: 300, ChannelId: 8203, Enabled: false, SortOrder: 1,
	}).Error)
	require.NoError(t, dbx.DB.Create(&ModelAliasContextRoute{
		Alias: "cache-solo", ContextMin: 0, ContextMax: 0, ChannelId: 8204, Enabled: false,
	}).Error)

	prevPolicies := dispatchPolicies
	prevContextRoutes := contextRoutesByAlias
	channelSyncLock.Lock()
	buildDispatchCachesFromDB()
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		dispatchPolicies = prevPolicies
		contextRoutesByAlias = prevContextRoutes
		channelSyncLock.Unlock()
	})

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	policy := getCachedDispatchPolicy("cache-a")
	require.NotNil(t, policy)
	assert.Equal(t, "context_range", policy.Mode)
	require.NotNil(t, policy.FallbackAlias)
	assert.Equal(t, "fallback-alias", *policy.FallbackAlias)

	policyB := getCachedDispatchPolicy("cache-b")
	require.NotNil(t, policyB)
	assert.Equal(t, "weighted_pool", policyB.Mode)
	assert.Nil(t, getCachedDispatchPolicy("cache-missing"),
		"an alias without a policy row reads as nil, which the caller treats as weighted_pool")

	routes := getCachedContextRoutes("cache-a")
	require.Len(t, routes, 2, "disabled rows must not enter the snapshot")
	assert.Equal(t, 10, routes[0].SortOrder)
	assert.Equal(t, 8202, routes[0].ChannelId)
	assert.Equal(t, 20, routes[1].SortOrder)
	assert.Equal(t, 8201, routes[1].ChannelId)

	assert.Nil(t, getCachedContextRoutes("cache-solo"), "an alias with only disabled rows has no context routes")
	assert.Empty(t, contextRoutesByAlias["cache-b"], "an alias without context routes has no snapshot entry")
}
