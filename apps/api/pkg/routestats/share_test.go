package routestats

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shareTestSetting returns a config with correction enabled at the shipped
// defaults, so the gates below measure the behaviour operators actually get.
func shareTestSetting(window int) *RouteStatsSetting {
	cfg := DefaultRouteStatsSetting()
	cfg.ShareWindowSize = window
	return cfg
}

func routeID(n int) RouteID {
	return RouteID{ChannelID: n, KeyIndex: 0, UpstreamModel: "upstream"}
}

// drawPool runs one weighted-random selection over base scores adjusted by the
// share correction, mirroring exactly what selectByWeight does: normalise the
// base scores into target shares, ask for corrections, pick by cumulative
// weight, then record the winner with the same target snapshot.
//
// available lets a test remove a route from the candidate set for a stretch of
// requests, which is what a state-machine ejection or a retry exclusion does.
func drawPool(t *testing.T, pool PoolKey, base map[RouteID]float64, available []RouteID, rng *rand.Rand, cfg *RouteStatsSetting) RouteID {
	t.Helper()

	targets := make(map[RouteID]float64, len(available))
	var total float64
	for _, id := range available {
		total += base[id]
	}
	require.Greater(t, total, 0.0, "pool must have positive base weight")
	for _, id := range available {
		targets[id] = base[id] / total
	}

	corr := Corrections(pool, targets, cfg)
	weights := make([]float64, len(available))
	var weightTotal float64
	for i, id := range available {
		weights[i] = base[id] * corr[id].Correction
		weightTotal += weights[i]
	}

	r := rng.Float64() * weightTotal
	winner := available[len(available)-1]
	for i, id := range available {
		r -= weights[i]
		if r < 0 {
			winner = id
			break
		}
	}
	RecordSelection(pool, winner, targets, cfg)
	return winner
}

// TestShareCorrectionDisabledByZeroWindow pins the A/B switch: window 0 must be
// a true no-op so the same binary can serve as the no-correction baseline. If
// this regresses, the load test compares two identical arms.
func TestShareCorrectionDisabledByZeroWindow(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	cfg := shareTestSetting(0)
	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	targets := map[RouteID]float64{routeID(1): 0.5, routeID(2): 0.5}

	for range 50 {
		RecordSelection(pool, routeID(1), targets, cfg)
	}

	assert.Zero(t, SharePoolCount(), "a disabled window must not allocate pool state")
	for id, c := range Corrections(pool, targets, cfg) {
		assert.Equal(t, 1.0, c.Correction, "route %v must get a neutral correction", id)
	}
}

// TestShareCorrectionConvergesToBaseScoreShare is gate G1: the correction must
// leave the steady-state distribution exactly where the base scores put it.
//
// This is why the correction compares against the base-score share rather than
// the static-weight share. Targeting static weight instead turns the correction
// into a negative feedback loop against the quality term and the pool converges
// to share proportional to weight*sqrt(quality) — for a route at the quality
// floor that is 41.4% instead of 33.3%, an 8pp error against a 0.5pp bound.
func TestShareCorrectionConvergesToBaseScoreShare(t *testing.T) {
	for _, tc := range []struct {
		name      string
		qualityB  float64
		wantShare float64
	}{
		{"both healthy", 1.000, 50.000},
		{"B ttft 2x", 0.875, 46.667},
		{"B ttft 2x and half tps", 0.800, 44.444},
		{"B throttled", 0.612, 37.965},
		{"B at quality floor", 0.500, 33.333},
		{"B four times faster", 1.125, 52.941},
	} {
		// G1 is an equivalence claim, so every row runs on both arms: the shipped
		// window and the A/B baseline that #418 compares against. One arm alone
		// would prove the correction converges somewhere, not that it converges
		// where the quality term already put it.
		for _, window := range []int{0, 200} {
			t.Run(fmt.Sprintf("%s/window=%d", tc.name, window), func(t *testing.T) {
				ResetShares()
				t.Cleanup(ResetShares)

				cfg := shareTestSetting(window)
				pool := PoolKey{Group: "g", PublicModelAlias: "m"}
				a, b := routeID(1), routeID(2)
				base := map[RouteID]float64{a: 100.0, b: 100.0 * tc.qualityB}
				available := []RouteID{a, b}

				rng := rand.New(rand.NewPCG(0x5EED, 0xC0FFEE))
				const warmup, sample = 2000, 8000
				for range warmup {
					drawPool(t, pool, base, available, rng, cfg)
				}
				hits := 0
				for range sample {
					if drawPool(t, pool, base, available, rng, cfg) == b {
						hits++
					}
				}

				got := 100 * float64(hits) / float64(sample)
				assert.InDelta(t, tc.wantShare, got, 0.5,
					"quality %.3f at window %d must yield %.3f%% share, got %.3f%%",
					tc.qualityB, window, tc.wantShare, got)
			})
		}
	}
}

// TestShareCorrectionPullsBackBypassedTraffic is gate G2 and the whole reason the
// window exists.
//
// Channel affinity, specific-channel requests and locked replay all serve a
// route unit without going through weighted random selection. The final score
// cannot influence traffic it never scores, so a pinned route runs away with far
// more than its configured share. Recording those requests into the window is the
// only mechanism that pulls it back — and it is also why the no-correction arm
// cannot pass this test.
func TestShareCorrectionPullsBackBypassedTraffic(t *testing.T) {
	const (
		bypassEveryN = 10 // 30% of traffic is pinned, expressed as 3 in every 10
		requests     = 6000
	)

	run := func(window int) float64 {
		ResetShares()
		t.Cleanup(ResetShares)

		cfg := shareTestSetting(window)
		pool := PoolKey{Group: "g", PublicModelAlias: "m"}
		ids := []RouteID{routeID(1), routeID(2), routeID(3)}
		base := map[RouteID]float64{ids[0]: 100, ids[1]: 100, ids[2]: 100}
		targets := map[RouteID]float64{ids[0]: 1.0 / 3, ids[1]: 1.0 / 3, ids[2]: 1.0 / 3}

		rng := rand.New(rand.NewPCG(0xA11CE, 7))
		served := 0
		for i := range requests {
			if i%bypassEveryN < 3 {
				// Pinned by affinity: no competition, but the pool still paid for it.
				RecordSelection(pool, ids[0], targets, cfg)
				served++
				continue
			}
			if drawPool(t, pool, base, ids, rng, cfg) == ids[0] {
				served++
			}
		}
		return 100 * float64(served) / float64(requests)
	}

	baseline := run(0)
	corrected := run(200)

	assert.Greater(t, baseline, 50.0,
		"without correction the pinned route must run away with the pool (measured ~53%%), got %.1f%%", baseline)
	assert.LessOrEqual(t, corrected, 48.0,
		"correction must pull the pinned route back under 48%%, got %.1f%%", corrected)
	assert.Less(t, corrected, baseline, "correction must reduce the pinned route's share")
}

// TestShareCorrectionDoesNotSpikeOnReEntry is gate G3, and it is the reason the
// window stores the target that was in force for each recorded request instead of
// recomputing entitlement from the current candidate set.
//
// A route ejected by the state machine contributes no opportunities while it is
// out, so on return its ratio is measured only over requests it could actually
// win. Dividing by the whole window instead makes a returning route look
// starved: its count is 1 against a 200-request window, the correction saturates
// at the maximum, and it is handed roughly 74% of the next 100 requests. Guarding
// on "never seen before" does not help, because the spike comes from a small
// non-zero count rather than a zero one.
func TestShareCorrectionDoesNotSpikeOnReEntry(t *testing.T) {
	// The gate's 55% line sits about one binomial sigma above the 50% fair share:
	// 100 requests at p=0.5 has sigma 5pp, so a single seed clears or trips the
	// line on noise alone (measured across 200 seeds: mean 50.2%, p95 55%, and
	// 4-8% of seeds above 55% at every window size from 50 to 1000). Averaging the
	// seeds measures the thing the gate is actually about — whether re-entry
	// produces a spike — and keeps a per-seed ceiling well clear of the naive
	// implementation's 74%.
	const seeds = 40
	total, worst := 0, 0
	for seed := range seeds {
		ResetShares()

		cfg := shareTestSetting(200)
		pool := PoolKey{Group: "g", PublicModelAlias: "m"}
		a, b := routeID(1), routeID(2)
		base := map[RouteID]float64{a: 100, b: 100}

		rng := rand.New(rand.NewPCG(uint64(seed)+1, 11))
		// B is ejected for more than a full window, so nothing about it survives.
		for range 500 {
			drawPool(t, pool, base, []RouteID{a}, rng, cfg)
		}

		hits := 0
		for range 100 {
			if drawPool(t, pool, base, []RouteID{a, b}, rng, cfg) == b {
				hits++
			}
		}
		total += hits
		if hits > worst {
			worst = hits
		}
	}
	t.Cleanup(ResetShares)

	mean := float64(total) / seeds
	assert.LessOrEqual(t, mean, 55.0,
		"a returning route must not be flooded: mean %.1f%% of the first 100 requests, fair share is 50%%", mean)
	assert.GreaterOrEqual(t, mean, 45.0,
		"a returning route must not be starved either: mean %.1f%%", mean)
	assert.Less(t, worst, 70,
		"no seed may approach the naive implementation's 74%% spike, worst was %d%%", worst)
}

// TestShareCorrectionClampBoundsHold covers W3.1's clamp requirement. The clamp
// only ever binds in transient conditions — in steady state the correction sits
// at 1.0 and never touches a bound — so the bound has to be provoked by forcing
// a route far away from its entitlement.
func TestShareCorrectionClampBoundsHold(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	cfg := shareTestSetting(200)
	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	a, b := routeID(1), routeID(2)
	targets := map[RouteID]float64{a: 0.5, b: 0.5}

	// Hand every selection to A: A is maximally over-served, B maximally starved.
	for range 200 {
		RecordSelection(pool, a, targets, cfg)
	}

	corr := Corrections(pool, targets, cfg)
	assert.Equal(t, cfg.ShareCorrMin, corr[a].Correction,
		"an over-served route must be derated down to the floor")
	assert.Equal(t, cfg.ShareCorrMax, corr[b].Correction,
		"a starved route must be boosted up to the ceiling")
	assert.Equal(t, 1.0, corr[a].ActualShare, "A took every selection")
	assert.Zero(t, corr[b].ActualShare, "B took none")
	assert.Equal(t, 200, corr[a].Opportunities)
}

// TestShareWindowEvictsOldestBeyondCapacity pins the ring buffer's bound: a pool
// under sustained traffic must not grow without limit, and the counters must
// reflect only the retained entries.
func TestShareWindowEvictsOldestBeyondCapacity(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	const window = 50
	cfg := shareTestSetting(window)
	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	a, b := routeID(1), routeID(2)
	targets := map[RouteID]float64{a: 0.5, b: 0.5}

	// Fill the window with A, then push exactly one window's worth of B through.
	for range window {
		RecordSelection(pool, a, targets, cfg)
	}
	require.Equal(t, window, Corrections(pool, targets, cfg)[a].Opportunities)
	for range window {
		RecordSelection(pool, b, targets, cfg)
	}

	corr := Corrections(pool, targets, cfg)
	assert.Equal(t, window, corr[a].Opportunities, "window must stay at capacity")
	assert.Zero(t, corr[a].Selections, "every A selection must have aged out")
	assert.Equal(t, window, corr[b].Selections, "B must own the whole window")
}

// TestShareWindowPartialFillIsUnbiased covers the cold-start boundary from W3.1.
// An unfilled window must not favour anyone: correction is driven by the ratio of
// selections to opportunities, both of which grow together, so a 20/80 pool holds
// its configured split from the first request rather than needing a warm-up.
func TestShareWindowPartialFillIsUnbiased(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	cfg := shareTestSetting(200)
	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	light, heavy := routeID(1), routeID(2)
	base := map[RouteID]float64{light: 20, heavy: 80}
	ids := []RouteID{light, heavy}

	rng := rand.New(rand.NewPCG(0xF00D, 3))
	hits := 0
	const requests = 2000 // an order of magnitude short of filling nothing; window stays partial early
	for range requests {
		if drawPool(t, pool, base, ids, rng, cfg) == light {
			hits++
		}
	}

	got := 100 * float64(hits) / float64(requests)
	assert.InDelta(t, 20.0, got, 2.0,
		"a 20/80 pool must hold its split through the partial-window phase, got %.2f%%", got)
}

// TestShareCorrectionScopedPerPool pins the pool boundary: selection draws from
// one (group, alias) pair, so two groups serving the same alias must not share a
// window. A leak here would let a busy group's traffic derate a quiet group's
// routes.
func TestShareCorrectionScopedPerPool(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	cfg := shareTestSetting(200)
	def := PoolKey{Group: "default", PublicModelAlias: "m"}
	vip := PoolKey{Group: "vip", PublicModelAlias: "m"}
	a, b := routeID(1), routeID(2)
	targets := map[RouteID]float64{a: 0.5, b: 0.5}

	for range 100 {
		RecordSelection(def, a, targets, cfg)
	}

	assert.Less(t, Corrections(def, targets, cfg)[a].Correction, 1.0,
		"the busy pool must derate its over-served route")
	assert.Equal(t, 1.0, Corrections(vip, targets, cfg)[a].Correction,
		"a different group must be untouched by the busy pool's history")
	assert.Equal(t, 2, SharePoolCount())
}

// TestSweepSharePoolsDropsUnknownPools covers the eviction hook: pools whose
// route rows are gone must not sit in memory forever.
func TestSweepSharePoolsDropsUnknownPools(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	cfg := shareTestSetting(200)
	keep := PoolKey{Group: "g", PublicModelAlias: "keep"}
	drop := PoolKey{Group: "g", PublicModelAlias: "drop"}
	targets := map[RouteID]float64{routeID(1): 1.0}
	RecordSelection(keep, routeID(1), targets, cfg)
	RecordSelection(drop, routeID(1), targets, cfg)
	require.Equal(t, 2, SharePoolCount())

	removed := SweepSharePools(map[PoolKey]struct{}{keep: {}})
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, SharePoolCount())
	assert.Equal(t, 1, Corrections(keep, targets, cfg)[routeID(1)].Opportunities,
		"the retained pool must keep its history")
}

// TestShareWindowShrinkDropsExcessHistory covers a live config change: lowering
// the window must take effect immediately rather than leaving stale entries that
// outlive the new capacity.
func TestShareWindowShrinkDropsExcessHistory(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	a, b := routeID(1), routeID(2)
	targets := map[RouteID]float64{a: 0.5, b: 0.5}

	wide := shareTestSetting(100)
	for range 100 {
		RecordSelection(pool, a, targets, wide)
	}
	require.Equal(t, 100, Corrections(pool, targets, wide)[a].Opportunities)

	narrow := shareTestSetting(10)
	for range 10 {
		RecordSelection(pool, b, targets, narrow)
	}

	corr := Corrections(pool, targets, narrow)
	assert.LessOrEqual(t, corr[b].Opportunities, 10,
		"history beyond the new capacity must be dropped, got %d", corr[b].Opportunities)
	assert.Equal(t, corr[b].Opportunities, corr[b].Selections,
		"only the post-shrink selections may remain")
}
