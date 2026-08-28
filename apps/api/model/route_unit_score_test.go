package model

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/pkg/routestats"
)

// withRouteStats installs a route stats config for one test and restores the
// previous one, so a window size or floor set here cannot leak into a sibling.
// Route state and share windows are cleared on both sides: EWMA samples are
// process-global, and a leftover sample would silently move another test's share.
func withRouteStats(t *testing.T, mutate func(cfg *routestats.RouteStatsSetting)) {
	t.Helper()
	previous := routestats.GetRouteStatsSetting()
	cfg := routestats.DefaultRouteStatsSetting()
	if mutate != nil {
		mutate(cfg)
	}
	routestats.Reset()
	routestats.ResetShares()
	routestats.SetRouteStatsSetting(cfg)
	t.Cleanup(func() {
		routestats.SetRouteStatsSetting(previous)
		routestats.Reset()
		routestats.ResetShares()
	})
}

// observeQuality drives a route handle to a known quality by feeding it explicit
// observations. It is the only honest way to test the wiring: reaching into the
// EWMA state would prove the test can write a float, not that selection reads it.
//
// successRate is applied as a first observation (the EWMA seeds to its first
// sample), and ttftMs/tps are applied the same way. Extra success samples top the
// count past MinSamples so ComputeQuality stops returning neutral 1.0.
func observeQuality(t *testing.T, key routestats.RouteKey, successRate, ttftMs, tps float64, samples int) {
	t.Helper()
	h := routestats.GetOrCreateHandle(key)
	require.NotNil(t, h)
	if ttftMs > 0 {
		h.ObserveTTFT(ttftMs)
	}
	if tps > 0 {
		h.ObserveTPS(tps)
	}
	for range samples {
		h.ObserveSuccess(successRate)
	}
}

func routeStatsKey(group, alias, upstream string, channelID int) routestats.RouteKey {
	return routestats.RouteKey{
		Group:            group,
		PublicModelAlias: alias,
		ChannelID:        channelID,
		KeyIndex:         0,
		UpstreamModel:    upstream,
	}
}

// drawShares runs the real selector n times against a deterministic source and
// returns the hit count per channel.
func drawShares(t *testing.T, group, alias string, n int, seed uint64) map[int]int {
	t.Helper()
	rnd := rand.New(rand.NewPCG(seed, 0x9E3779B9))
	counts := map[int]int{}
	for range n {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		counts[selected.ChannelId]++
	}
	return counts
}

// ---- W1: static weight baseline ----

// TestScoreW1StaticWeightBaseline is W1.1: with equal quality and health, traffic
// must land on the configured static-weight split and nothing else.
func TestScoreW1StaticWeightBaseline(t *testing.T) {
	const group, alias = "w1-group", "w1-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chLight := testRouteChannel(7101, false, []string{"sk-l"}, nil)
	chHeavy := testRouteChannel(7102, false, []string{"sk-h"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chLight, chHeavy}, group, alias, []ChannelModelRoute{
		testRoute(1, 7101, 0, group, alias, "up-light", 20),
		testRoute(2, 7102, 0, group, alias, "up-heavy", 80),
	})
	defer cleanup()

	const draws = 10000
	counts := drawShares(t, group, alias, draws, 0x5EED)

	// routingBaseWeight adds one to each configured weight, so the exact expected
	// split is 21:81 rather than 20:80.
	wantLight := 100 * 21.0 / 102.0
	gotLight := 100 * float64(counts[7101]) / float64(draws)
	assert.InDelta(t, wantLight, gotLight, 1.5,
		"weight 20 vs 80 must yield ~%.1f%%, got %.2f%%", wantLight, gotLight)
}

// TestScoreW1ZeroTotalWeight is W1.2: an all-zero-weight pool must stay usable.
// routingBaseWeight's +1 offset is what makes this equiprobable instead of a
// division by zero or an empty candidate set.
func TestScoreW1ZeroTotalWeight(t *testing.T) {
	const group, alias = "w1z-group", "w1z-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7111, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7112, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7111, 0, group, alias, "up-a", 0),
		testRoute(2, 7112, 0, group, alias, "up-b", 0),
	})
	defer cleanup()

	counts := drawShares(t, group, alias, 2000, 0xC0FFEE)
	assert.InDelta(t, 1000, counts[7111], 120, "zero-weight routes must be equiprobable")
	assert.InDelta(t, 1000, counts[7112], 120, "zero-weight routes must be equiprobable")
}

// TestScoreW1SingleCandidateShortCircuit is W1.3: a lone candidate is returned
// regardless of quality. Scaling a share that is already 100% cannot change the
// outcome, and a floor-quality sole provider must not be starved out.
func TestScoreW1SingleCandidateShortCircuit(t *testing.T) {
	const group, alias = "w1s-group", "w1s-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	ch := testRouteChannel(7121, false, []string{"sk-only"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, []ChannelModelRoute{
		testRoute(1, 7121, 0, group, alias, "up-only", 100),
	})
	defer cleanup()

	// Drive it to the quality floor: every attempt failed.
	observeQuality(t, routeStatsKey(group, alias, "up-only", 7121), 0, 120000, 0, 8)
	require.InDelta(t, 0.5, routestats.GetOrCreateHandle(routeStatsKey(group, alias, "up-only", 7121)).Quality().Quality, 1e-9)

	counts := drawShares(t, group, alias, 50, 0xBEEF)
	assert.Equal(t, 50, counts[7121], "the only candidate must always be served")
}

// ---- W2: quality wiring ----

// TestScoreW2QualityDrivesShare is W2.1: the six-row acceptance table, asserted
// against the real selector rather than against a model of it.
//
// Each row drives route B's EWMA to a known quality with real observations, then
// measures the share it wins. The expected values come from the agreed synthesis
// weights (success 0.60, ttft 0.25, tps 0.15) with per-component clamp [0.2, 1.5]
// and synthesis clamp [0.5, 1.5].
func TestScoreW2QualityDrivesShare(t *testing.T) {
	for _, tc := range []struct {
		name        string
		successRate float64
		ttftMs      float64
		tps         float64
		wantQuality float64
		wantShareB  float64
	}{
		{"both healthy", 1.0, 2000, 20, 1.000, 50.0},
		{"B ttft twice target", 1.0, 4000, 20, 0.875, 46.7},
		{"B ttft twice and tps half", 1.0, 4000, 10, 0.800, 44.4},
		// Sustained 429s: success decays to 0.7 and the retry backoff shows up as a
		// 5s TTFT, with no TPS sample because nothing streamed. Weights renormalise
		// over the two observed components: (0.60*0.7 + 0.25*0.4)/0.85 = 0.612.
		{"B throttled by 429s", 0.7, 5000, 0, 0.612, 38.0},
		{"B four times faster", 1.0, 500, 20, 1.125, 52.9},
		{"B every attempt failed", 0.0, 120000, 0, 0.500, 33.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const group, alias = "w2-group", "w2-model"
			withRouteStats(t, nil)
			ClearRouteHealthCache()
			t.Cleanup(ClearRouteHealthCache)

			chA := testRouteChannel(7201, false, []string{"sk-a"}, nil)
			chB := testRouteChannel(7202, false, []string{"sk-b"}, nil)
			cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
				testRoute(1, 7201, 0, group, alias, "up-a", 100),
				testRoute(2, 7202, 0, group, alias, "up-b", 100),
			})
			defer cleanup()

			// A is exactly on target, so its quality is 1.0 and it is the reference.
			observeQuality(t, routeStatsKey(group, alias, "up-a", 7201), 1.0, 2000, 20, 8)
			observeQuality(t, routeStatsKey(group, alias, "up-b", 7202), tc.successRate, tc.ttftMs, tc.tps, 8)

			gotQ := routestats.GetOrCreateHandle(routeStatsKey(group, alias, "up-b", 7202)).Quality().Quality
			require.InDelta(t, tc.wantQuality, gotQ, 0.01,
				"fixture must reach quality %.3f before share is meaningful, got %.4f", tc.wantQuality, gotQ)

			// W2.1 demands the share land within 0.5pp of the analytic value. At
			// p≈0.5 the sampling error is ~0.25pp/√(n/40000), so 40k draws put 2σ
			// inside the bound rather than leaving the gate to seed luck.
			const draws = 40000
			counts := drawShares(t, group, alias, draws, 0xA11CE)
			gotShare := 100 * float64(counts[7202]) / float64(draws)
			assert.InDelta(t, tc.wantShareB, gotShare, 0.5,
				"quality %.3f must yield %.1f%% share within 0.5pp, got %.2f%%", tc.wantQuality, tc.wantShareB, gotShare)
		})
	}
}

// TestScoreW2QualityNeverStarvesARoute is W2.2: the synthesis floor is what keeps
// EWMA a preference rather than an execution. A route whose every attempt failed
// sits at quality 0.5 and still wins roughly a third of a two-route pool, because
// eliminating a route is the state machine's job and its alone.
func TestScoreW2QualityNeverStarvesARoute(t *testing.T) {
	const group, alias = "w2f-group", "w2f-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7211, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7212, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7211, 0, group, alias, "up-a", 100),
		testRoute(2, 7212, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	observeQuality(t, routeStatsKey(group, alias, "up-a", 7211), 1.0, 2000, 20, 8)
	observeQuality(t, routeStatsKey(group, alias, "up-b", 7212), 0.0, 120000, 0, 30)

	q := routestats.GetOrCreateHandle(routeStatsKey(group, alias, "up-b", 7212)).Quality().Quality
	assert.InDelta(t, 0.5, q, 1e-9, "synthesis must clamp at the floor, not fall to zero")

	counts := drawShares(t, group, alias, 3000, 0xF00D)
	assert.Positive(t, counts[7212], "EWMA alone must never remove a route from the pool")
	share := 100 * float64(counts[7212]) / 3000.0
	assert.InDelta(t, 33.3, share, 2.0, "a floor-quality route keeps ~1/3 of a two-route pool, got %.2f%%", share)
}

// TestScoreW2PoolSizeChangesTheLoss is W2.3: the same bad route loses a very
// different amount of share depending on pool size, so the two-route number is
// not a general acceptance line. 33.3% / 14.3% / 6.67% for pools of 2 / 4 / 8.
func TestScoreW2PoolSizeChangesTheLoss(t *testing.T) {
	for _, tc := range []struct {
		poolSize  int
		wantShare float64
	}{
		{2, 33.3},
		{4, 14.3},
		{8, 6.67},
	} {
		t.Run("pool", func(t *testing.T) {
			group := "w2p-group"
			alias := "w2p-model"
			withRouteStats(t, nil)
			ClearRouteHealthCache()
			t.Cleanup(ClearRouteHealthCache)

			channels := make([]*Channel, 0, tc.poolSize)
			routes := make([]ChannelModelRoute, 0, tc.poolSize)
			for i := range tc.poolSize {
				id := 7300 + tc.poolSize*10 + i
				channels = append(channels, testRouteChannel(id, false, []string{"sk"}, nil))
				routes = append(routes, testRoute(i+1, id, 0, group, alias, "up", 100))
			}
			cleanup := withRouteUnitFixture(t, channels, group, alias, routes)
			defer cleanup()

			// Every route shares upstream model "up", so their stats keys differ only
			// by channel id — which is exactly how route units are identified.
			badID := 7300 + tc.poolSize*10 + tc.poolSize - 1
			for i := range tc.poolSize {
				id := 7300 + tc.poolSize*10 + i
				if id == badID {
					observeQuality(t, routeStatsKey(group, alias, "up", id), 0.0, 120000, 0, 10)
					continue
				}
				observeQuality(t, routeStatsKey(group, alias, "up", id), 1.0, 2000, 20, 10)
			}

			const draws = 16000
			counts := drawShares(t, group, alias, draws, 0xDEC0DE)
			got := 100 * float64(counts[badID]) / float64(draws)
			assert.InDelta(t, tc.wantShare, got, 1.5,
				"pool of %d: floor-quality route must take ~%.2f%%, got %.2f%%", tc.poolSize, tc.wantShare, got)
		})
	}
}

// TestScoreW2LatencySignalIsBounded is W2.4: pure latency differences move share
// by a bounded amount and the ordering is strict. The per-component floor of 0.2
// caps how much a slow route can be punished on latency alone, which is why 4x
// and 10x are distinguishable but 10x and 60x are not.
func TestScoreW2LatencySignalIsBounded(t *testing.T) {
	const group, alias = "w2l-group", "w2l-model"

	shareAt := func(t *testing.T, ttftMultiple float64) float64 {
		t.Helper()
		withRouteStats(t, nil)
		ClearRouteHealthCache()
		t.Cleanup(ClearRouteHealthCache)

		chA := testRouteChannel(7241, false, []string{"sk-a"}, nil)
		chB := testRouteChannel(7242, false, []string{"sk-b"}, nil)
		cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
			testRoute(1, 7241, 0, group, alias, "up-a", 100),
			testRoute(2, 7242, 0, group, alias, "up-b", 100),
		})
		defer cleanup()

		// success is fully healthy on both sides: latency is the only difference.
		observeQuality(t, routeStatsKey(group, alias, "up-a", 7241), 1.0, 2000, 20, 8)
		observeQuality(t, routeStatsKey(group, alias, "up-b", 7242), 1.0, 2000*ttftMultiple, 20, 8)

		const draws = 12000
		counts := drawShares(t, group, alias, draws, 0x1234)
		return 100 * float64(counts[7242]) / float64(draws)
	}

	var got [3]float64
	for i, mult := range []float64{2, 4, 10} {
		got[i] = shareAt(t, mult)
	}

	assert.Greater(t, got[0], got[1], "4x must lose more share than 2x")
	assert.Greater(t, got[1], got[2], "10x must lose more share than 4x")
	assert.Greater(t, got[2], 40.0,
		"the component floor bounds the latency penalty: even 10x keeps >40%%, got %.2f%%", got[2])
}

// ---- W3: health multiplier and correction ----

// TestScoreW3DisabledRouteScoresZero is W3.3: disabled is the one state that
// removes a route from the pool, and it does so through the health multiplier
// rather than through quality.
func TestScoreW3DisabledRouteScoresZero(t *testing.T) {
	const group, alias = "w3d-group", "w3d-model"
	withRouteStats(t, nil)
	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7301, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7302, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7301, 0, group, alias, "up-a", 100),
		testRoute(2, 7302, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	require.NoError(t, DisableRoute(RouteKey{ChannelId: 7302, KeyIndex: 0, Model: alias}, time.Now()))

	counts := drawShares(t, group, alias, 300, 0xDEAD)
	assert.Zero(t, counts[7302], "a disabled route must never be selected")
	assert.Equal(t, 300, counts[7301])
}

// TestScoreW3CalmRouteKeepsReducedShare is W3.3's other half: isolation is graded,
// not binary. A calm route stays selectable at the configured scale, which is the
// natural probe that lets it recover.
func TestScoreW3CalmRouteKeepsReducedShare(t *testing.T) {
	const group, alias = "w3c-group", "w3c-model"
	withRouteStats(t, nil)
	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7311, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7312, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7311, 0, group, alias, "up-a", 100),
		testRoute(2, 7312, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	require.NoError(t, RecordRetryableFailure(
		RouteKey{ChannelId: 7312, KeyIndex: 0, Model: alias}, "bad_response", FailureSourceUpstream, time.Now()))
	require.InDelta(t, 0.5, RouteWeightMultiplier(RouteKey{ChannelId: 7312, KeyIndex: 0, Model: alias}), 1e-9,
		"fixture must land the route in calm at 50%% weight")

	const draws = 6000
	counts := drawShares(t, group, alias, draws, 0xFEED)
	assert.Positive(t, counts[7312], "calm must stay selectable so it can recover")
	assert.Greater(t, counts[7311], counts[7312], "the healthy peer must take the majority")
	share := 100 * float64(counts[7312]) / float64(draws)
	assert.InDelta(t, 33.3, share, 2.5,
		"a half-weight route takes ~1/3 of a two-route pool, got %.2f%%", share)
}

// TestScoreW3SignalsDoNotDoublePenalise is W3.2: one failure must move the two
// signals independently, and neither may reach into the other's range. Quality is
// bounded below by its floor and cannot eject; health can reach zero and is the
// only thing that may.
func TestScoreW3SignalsDoNotDoublePenalise(t *testing.T) {
	const group, alias = "w3s-group", "w3s-model"
	withRouteStats(t, nil)
	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	ch := testRouteChannel(7321, false, []string{"sk-a"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{ch}, group, alias, []ChannelModelRoute{
		testRoute(1, 7321, 0, group, alias, "up-a", 100),
	})
	defer cleanup()

	key := RouteKey{ChannelId: 7321, KeyIndex: 0, Model: alias}
	statsKey := routeStatsKey(group, alias, "up-a", 7321)

	// Soft signal only: 30 failed attempts.
	observeQuality(t, statsKey, 0.0, 120000, 0, 30)
	assert.InDelta(t, 0.5, routestats.GetOrCreateHandle(statsKey).Quality().Quality, 1e-9,
		"quality bottoms out at its floor no matter how many failures arrive")
	assert.Equal(t, 1.0, RouteWeightMultiplier(key),
		"EWMA observations must not move the state machine")

	// Hard signal only: one retry-eligible failure.
	require.NoError(t, RecordRetryableFailure(key, "bad_response", FailureSourceUpstream, time.Now()))
	assert.Less(t, RouteWeightMultiplier(key), 1.0, "the state machine derates independently")
	assert.InDelta(t, 0.5, routestats.GetOrCreateHandle(statsKey).Quality().Quality, 1e-9,
		"a state transition must not additionally move quality")
}

// TestScoreW3SafeDegradation is W3.4: a poisoned quality value must cost only the
// route that carries it. A NaN reaching the cumulative pick would bias every
// candidate after it, so the score is sanitised per candidate instead.
func TestScoreW3SafeDegradation(t *testing.T) {
	const group, alias = "w3n-group", "w3n-model"
	// Component and quality ceilings of +Inf make the normalised quality
	// unbounded, which is the cheapest way to force a non-finite score through
	// the public config surface rather than by reaching into private state.
	withRouteStats(t, func(cfg *routestats.RouteStatsSetting) {
		cfg.ComponentCeil = math.Inf(1)
		cfg.QualityCeil = math.Inf(1)
	})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7331, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7332, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7331, 0, group, alias, "up-a", 100),
		testRoute(2, 7332, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	// A TTFT of zero drives the "lower is better" normaliser to its ceiling, which
	// is now +Inf.
	observeQuality(t, routeStatsKey(group, alias, "up-a", 7331), 1.0, 2000, 20, 8)
	hB := routestats.GetOrCreateHandle(routeStatsKey(group, alias, "up-b", 7332))
	hB.ObserveTTFT(0)
	for range 8 {
		hB.ObserveSuccess(1.0)
	}

	// Selection must still terminate and still return a real route.
	for range 200 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rand.New(rand.NewPCG(1, 2)))
		require.NoError(t, err)
		require.NotNil(t, selected, "a non-finite score must not empty the pool")
	}
}

// TestScoreW3AllZeroCandidatesYieldNoRoute pins the other degradation edge: when
// every candidate scores zero there is nothing to serve, and selection must say so
// rather than returning an arbitrary route.
func TestScoreW3AllZeroCandidatesYieldNoRoute(t *testing.T) {
	const group, alias = "w3z-group", "w3z-model"
	withRouteStats(t, nil)
	withRouteHealthDB(t)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7341, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7342, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7341, 0, group, alias, "up-a", 100),
		testRoute(2, 7342, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	now := time.Now()
	require.NoError(t, DisableRoute(RouteKey{ChannelId: 7341, KeyIndex: 0, Model: alias}, now))
	require.NoError(t, DisableRoute(RouteKey{ChannelId: 7342, KeyIndex: 0, Model: alias}, now))

	selected, err := SelectRouteUnit(group, alias, "", 0, nil, rand.New(rand.NewPCG(3, 4)))
	require.NoError(t, err)
	assert.Nil(t, selected, "a fully disabled pool must yield no route, not a fallback pick")
}

// ---- W4: exploration and cold start ----

// TestScoreW4ColdStartIsNeutral is W4.1: a route with no history must compete on
// its configured weight alone. Treating an unmeasured route as bad would starve
// every newly added channel; treating it as perfect would flood it.
func TestScoreW4ColdStartIsNeutral(t *testing.T) {
	const group, alias = "w4c-group", "w4c-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chOld := testRouteChannel(7401, false, []string{"sk-o"}, nil)
	chNew := testRouteChannel(7402, false, []string{"sk-n"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chOld, chNew}, group, alias, []ChannelModelRoute{
		testRoute(1, 7401, 0, group, alias, "up-o", 100),
		testRoute(2, 7402, 0, group, alias, "up-n", 100),
	})
	defer cleanup()

	// The established route is exactly on target; the new one has never served.
	observeQuality(t, routeStatsKey(group, alias, "up-o", 7401), 1.0, 2000, 20, 8)

	const draws = 8000
	counts := drawShares(t, group, alias, draws, 0x77777)
	share := 100 * float64(counts[7402]) / float64(draws)
	assert.InDelta(t, 50.0, share, 2.0,
		"an unmeasured route competes at neutral quality, got %.2f%%", share)
}

// TestScoreW4BelowMinSamplesStaysNeutral is the MinSamples gate: a couple of bad
// requests must not be enough to derate a route, or a transient blip would move
// traffic on no evidence.
func TestScoreW4BelowMinSamplesStaysNeutral(t *testing.T) {
	const group, alias = "w4m-group", "w4m-model"
	withRouteStats(t, nil)
	cfg := routestats.GetRouteStatsSetting()
	require.Positive(t, cfg.MinSamples)

	key := routeStatsKey(group, alias, "up-b", 7412)
	observeQuality(t, key, 0.0, 120000, 0, cfg.MinSamples-1)
	assert.Equal(t, 1.0, routestats.GetOrCreateHandle(key).Quality().Quality,
		"under MinSamples the synthesis must stay neutral")

	routestats.GetOrCreateHandle(key).ObserveSuccess(0.0)
	assert.Less(t, routestats.GetOrCreateHandle(key).Quality().Quality, 1.0,
		"crossing MinSamples must let the real quality through")
}

// TestScoreW4FloorQualityRouteRecovers is W4.2: a route at the quality floor must
// still be sampled often enough to climb back. The synthesis floor speeds recovery
// up; the per-component floor is what makes recovery possible at all, since with
// both floors at zero the score is zero and the route is never sampled again.
func TestScoreW4FloorQualityRouteRecovers(t *testing.T) {
	const group, alias = "w4r-group", "w4r-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7421, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7422, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7421, 0, group, alias, "up-a", 100),
		testRoute(2, 7422, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	badKey := routeStatsKey(group, alias, "up-b", 7422)
	observeQuality(t, routeStatsKey(group, alias, "up-a", 7421), 1.0, 2000, 20, 8)
	observeQuality(t, badKey, 0.0, 120000, 0, 20)
	require.InDelta(t, 0.5, routestats.GetOrCreateHandle(badKey).Quality().Quality, 1e-9)

	// The route now succeeds. Every time selection picks it, record a success and
	// count how many pool requests recovery took.
	rnd := rand.New(rand.NewPCG(0x9999, 0x1111))
	samples, requests := 0, 0
	for requests < 5000 {
		selected, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		require.NotNil(t, selected)
		requests++
		if selected.ChannelId != 7422 {
			continue
		}
		samples++
		h := routestats.GetOrCreateHandle(badKey)
		h.ObserveSuccess(1.0)
		h.ObserveTTFT(2000)
		h.ObserveTPS(20)
		if h.Quality().Quality > 0.9 {
			break
		}
	}

	q := routestats.GetOrCreateHandle(badKey).Quality().Quality
	assert.Greater(t, q, 0.9, "a floor-quality route must be able to climb back, reached %.4f", q)
	assert.Positive(t, samples, "recovery requires the route to keep receiving traffic")
	assert.Less(t, requests, 1000,
		"recovery must be prompt, took %d pool requests and %d samples", requests, samples)
}

// TestScoreW4ComponentFloorIsWhatPreventsStarvation separates the two floors,
// which the issue text originally conflated. With both floors removed the score
// collapses to zero and the route is never sampled again — that is a permanent
// starvation, not a slow recovery.
func TestScoreW4ComponentFloorIsWhatPreventsStarvation(t *testing.T) {
	const group, alias = "w4f-group", "w4f-model"
	withRouteStats(t, func(cfg *routestats.RouteStatsSetting) {
		cfg.ComponentFloor = 0
		cfg.QualityFloor = 0
	})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7431, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7432, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7431, 0, group, alias, "up-a", 100),
		testRoute(2, 7432, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	badKey := routeStatsKey(group, alias, "up-b", 7432)
	observeQuality(t, routeStatsKey(group, alias, "up-a", 7431), 1.0, 2000, 20, 8)
	// success 0 with no other observed component drives synthesis to zero. It lands
	// a hair above exact zero because staleness regression nudges the stored rate
	// back towards neutral between observations, which is immaterial here: the
	// resulting score is ~1e-9 of the pool and the route is never drawn again.
	h := routestats.GetOrCreateHandle(badKey)
	for range 20 {
		h.ObserveSuccess(0.0)
	}
	require.Less(t, h.Quality().Quality, 1e-6,
		"with both floors at zero quality collapses to effectively zero")

	// The route is starved, not mathematically excluded: quality lands ~1e-9 rather
	// than exactly 0 because staleness regression nudges the stored success rate
	// back towards neutral between observations. A share that small can still win a
	// draw once in a few thousand, and it does so depending on how much wall time
	// earlier tests consumed — asserting an exact zero made this order-dependent.
	// One hit in 2000 is starvation; the floor exists to prevent exactly this.
	counts := drawShares(t, group, alias, 2000, 0xABCD)
	assert.LessOrEqual(t, counts[7432], 2,
		"a route at zero quality is starved out of the pool: this is why the component floor exists")
}

// ---- W5: correction observability through selection ----

// TestScoreW5CorrectionIsNeutralAtConvergence is the invariant behind W5.1's
// hand-recomputation: once traffic matches the base-score distribution the
// correction contributes nothing, so the steady state is the base-score split and
// not something the window invented.
func TestScoreW5CorrectionIsNeutralAtConvergence(t *testing.T) {
	const group, alias = "w5-group", "w5-model"
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7501, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7502, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7501, 0, group, alias, "up-a", 100),
		testRoute(2, 7502, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	observeQuality(t, routeStatsKey(group, alias, "up-a", 7501), 1.0, 2000, 20, 8)
	observeQuality(t, routeStatsKey(group, alias, "up-b", 7502), 1.0, 4000, 20, 8)

	drawShares(t, group, alias, 4000, 0x5150)

	pool := routestats.PoolKey{Group: group, PublicModelAlias: alias}
	candidates := getCandidatesFromCache(group, alias)
	scores, _ := scoreCandidates(pool, candidates, alias)
	require.Len(t, scores, 2)

	for id, s := range scores {
		assert.InDelta(t, 1.0, s.Correction, 0.15,
			"route %v: correction must settle near 1.0 at convergence, got %.4f", id, s.Correction)
		assert.InDelta(t, s.BaseWeight*s.Quality*s.Health*s.Correction, s.Final, 1e-9,
			"route %v: final must be the product of its factors", id)
		assert.Positive(t, s.Opportunities, "route %v must have window history", id)
	}
}

// TestScoreW5WindowZeroDisablesCorrection pins the A/B switch end to end: with the
// window off the selector must still work and every correction must be exactly
// neutral, so the two load-test arms differ only in this one setting.
func TestScoreW5WindowZeroDisablesCorrection(t *testing.T) {
	const group, alias = "w5z-group", "w5z-model"
	withRouteStats(t, func(cfg *routestats.RouteStatsSetting) {
		cfg.ShareWindowSize = 0
	})
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	chA := testRouteChannel(7511, false, []string{"sk-a"}, nil)
	chB := testRouteChannel(7512, false, []string{"sk-b"}, nil)
	cleanup := withRouteUnitFixture(t, []*Channel{chA, chB}, group, alias, []ChannelModelRoute{
		testRoute(1, 7511, 0, group, alias, "up-a", 100),
		testRoute(2, 7512, 0, group, alias, "up-b", 100),
	})
	defer cleanup()

	observeQuality(t, routeStatsKey(group, alias, "up-a", 7511), 1.0, 2000, 20, 8)
	observeQuality(t, routeStatsKey(group, alias, "up-b", 7512), 1.0, 4000, 20, 8)

	const draws = 12000
	counts := drawShares(t, group, alias, draws, 0x2222)
	share := 100 * float64(counts[7512]) / float64(draws)
	assert.InDelta(t, 46.7, share, 1.5,
		"the no-correction arm must still track quality, got %.2f%%", share)

	assert.Zero(t, routestats.SharePoolCount(), "a disabled window must allocate no pool state")

	pool := routestats.PoolKey{Group: group, PublicModelAlias: alias}
	scores, _ := scoreCandidates(pool, getCandidatesFromCache(group, alias), alias)
	for id, s := range scores {
		assert.Equal(t, 1.0, s.Correction, "route %v must carry a neutral correction", id)
	}
}

// TestScoreW5ExpectedShareIsScopedPerGroup is W5.2 and the regression guard for
// F1. One channel can serve the same alias in several groups, and
// ExpandChannelModelRoutes emits one row per group, so the alias has two rows
// while selection still draws from a single (group, alias) pool.
//
// Two things have to hold per group, not per alias: expected_share must use the
// group's own weight total, and the six-factor breakdown must belong to that
// group's pool. RouteID carries no group, so both rows share an identity — a
// lookup keyed on it across pools silently reports one pool's scores twice.
func TestScoreW5ExpectedShareIsScopedPerGroup(t *testing.T) {
	cleanupDB := withRouteDB(t)
	defer cleanupDB()
	withRouteStats(t, nil)
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)

	const alias = "shared-model"
	ch := makeSingleKeyChannel(1, alias, "default,vip", nil)
	ch.Name = "dual-group"
	require.NoError(t, DB.Create(ch).Error)
	require.NoError(t, SeedChannelModelRoutes())

	views, err := GetRouteUnitViewsByAlias(alias)
	require.NoError(t, err)
	require.Len(t, views, 2, "one row per group")

	byGroup := make(map[string]RouteUnitView, len(views))
	for _, v := range views {
		byGroup[v.Group] = v
	}
	require.Contains(t, byGroup, "default")
	require.Contains(t, byGroup, "vip")

	// Give the two pools different histories. Quality alone is not enough to catch
	// a cross-pool overwrite: both views already carry their own quality from the
	// GetHandle pass, and an overwritten view keeps a self-consistent product. The
	// window counters are the discriminator, because they are only ever written by
	// the pool loop — vip gets three recorded requests, default none.
	observeQuality(t, routeStatsKey("vip", alias, alias, 1), 1.0, 4000, 20, 8)
	vipPool := routestats.PoolKey{Group: "vip", PublicModelAlias: alias}
	vipRoute := routestats.RouteID{ChannelID: 1, KeyIndex: 0, UpstreamModel: alias}
	cfg := routestats.GetRouteStatsSetting()
	for range 3 {
		routestats.RecordSelection(vipPool, vipRoute, map[routestats.RouteID]float64{vipRoute: 1.0}, cfg)
	}

	views, err = GetRouteUnitViewsByAlias(alias)
	require.NoError(t, err)
	byGroup = make(map[string]RouteUnitView, len(views))
	for _, v := range views {
		byGroup[v.Group] = v
	}

	for group, v := range byGroup {
		assert.InDelta(t, 1.0, v.ExpectedShare, 0.0001,
			"group %q holds the alias alone, so its route is entitled to all of it", group)
		// W5.1's hand-recompute must survive the multi-group case.
		assert.InDelta(t, v.BaseWeight*v.EwmaQuality*v.HealthMultiplier*v.ShareCorrection, v.FinalScore, 1e-9,
			"group %q: final score must be the product of the five reported factors", group)
	}

	assert.InDelta(t, 1.0, byGroup["default"].EwmaQuality, 1e-9,
		"the untouched group must stay neutral")
	assert.InDelta(t, 0.875, byGroup["vip"].EwmaQuality, 0.01,
		"the observed group must report its own quality, not the sibling group's")
	assert.Greater(t, byGroup["default"].FinalScore, byGroup["vip"].FinalScore,
		"a per-group breakdown must show the degraded pool scoring lower")
	assert.Equal(t, 3, byGroup["vip"].ShareOpportunities,
		"vip must report its own window history")
	assert.Equal(t, 3, byGroup["vip"].ShareSelections)
	assert.Zero(t, byGroup["default"].ShareOpportunities,
		"default served nothing, so borrowing vip's window counters is a reporting bug")
	assert.Zero(t, byGroup["default"].ShareSelections)
}
