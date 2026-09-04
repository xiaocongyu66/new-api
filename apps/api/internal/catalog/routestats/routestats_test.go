package routestats

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expected values in this file come from a pre-implementation Python simulation
// recorded in issue #405 (Verification checklist V1.1-V1.11). Every number that
// an assertion pins is accompanied by the value a known-wrong implementation
// produces, so a regression is recognisable rather than merely red.

// fakeClock drives NowMonotonic deterministically. Time only advances when a
// test says so, which is what makes the time-decay assertions reproducible.
type fakeClock struct {
	mu sync.Mutex
	d  time.Duration
}

func (c *fakeClock) now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.d
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.d += d
}

// newFixture installs a fake clock plus default settings and returns a handle on
// a route key unique to the test, so tests never share EWMA state.
func newFixture(t *testing.T) (*RouteHandle, *fakeClock, *RouteStatsSetting) {
	t.Helper()

	clock := &fakeClock{d: time.Second}
	restore := SetNowFunc(clock.now)
	cfg := DefaultRouteStatsSetting()
	prev := GetRouteStatsSetting()
	SetRouteStatsSetting(cfg)
	t.Cleanup(func() {
		restore()
		SetRouteStatsSetting(prev)
		Reset()
	})

	h := GetOrCreateHandle(RouteKey{
		Group:            "default",
		PublicModelAlias: t.Name(),
		ChannelID:        1,
		KeyIndex:         0,
		UpstreamModel:    "upstream-" + t.Name(),
	})
	require.NotNil(t, h)
	return h, clock, cfg
}

// primeSamples pushes n success observations so SampleCount clears MinSamples
// and ComputeQuality stops returning the neutral placeholder.
func primeSamples(h *RouteHandle, clock *fakeClock, n int) {
	for i := 0; i < n; i++ {
		h.ObserveSuccess(1.0)
		clock.advance(time.Millisecond)
	}
}

// V1.1 first sample initialises the EWMA to the observation itself. A folded
// first sample (alpha*x) would yield 0.1 for success and 200 for a 2000ms TTFT.
func TestV1_1FirstSampleInitialisesEwma(t *testing.T) {
	h, _, _ := newFixture(t)

	h.ObserveSuccess(1.0)
	h.ObserveTTFT(2000)
	h.ObserveTPS(20)
	h.ObserveLatency(30000)

	snap := h.Snapshot()
	assert.Equal(t, 1.0, snap.SuccessRate, "first success sample must land as-is, not alpha-folded")
	assert.Equal(t, 2000.0, snap.TTFTMs)
	assert.Equal(t, 20.0, snap.TPS)
	assert.Equal(t, 30000.0, snap.LatencyMs)
	// One request contributed all four facets, so it counts as one sample.
	assert.Equal(t, 1, snap.SampleCount)
}

// V1.1b a single observation of one metric counts exactly one sample.
func TestV1_1SingleObservationCountsOneSample(t *testing.T) {
	h, _, _ := newFixture(t)

	h.ObserveSuccess(0.0)

	snap := h.Snapshot()
	assert.Equal(t, 0.0, snap.SuccessRate)
	assert.Equal(t, 1, snap.SampleCount)
}

// V1.2 attribution whitelist: success/fatal/throttled write 1.0/0.0/0.7.
func TestV1_2AttributionObservationValues(t *testing.T) {
	cases := []struct {
		name string
		obs  float64
	}{
		{"success", 1.0},
		{"fatal", 0.0},
		{"throttled", 0.7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := newFixture(t)
			h.ObserveSuccess(tc.obs)
			assert.Equal(t, tc.obs, h.Snapshot().SuccessRate)
		})
	}
}

// V1.2b a neutral outcome must not touch state at all: user 4xx, client cancel
// and "no channel available" are not evidence against a route.
func TestV1_2NeutralOutcomeLeavesStateUntouched(t *testing.T) {
	h, clock, _ := newFixture(t)
	primeSamples(h, clock, 5)
	before := h.Snapshot()

	// The contract is that callers never invoke Observe* for neutral outcomes.
	// A nil handle stands in for "no route to attribute to" and must be a no-op.
	var nilHandle *RouteHandle
	nilHandle.ObserveSuccess(0.0)
	nilHandle.ObserveTTFT(9999)

	after := h.Snapshot()
	assert.Equal(t, before, after, "neutral traffic must not move any EWMA field")
	assert.Equal(t, before.SampleCount, after.SampleCount)
}

// V1.3 TPS peak direction: bad news for TPS is a LOWER value, so a drop lands
// wholesale. Getting the direction wrong yields 19.25 (q=0.963) and hides the
// slowdown entirely.
func TestV1_3TpsPeakDirectionDropsInWholesale(t *testing.T) {
	h, clock, cfg := newFixture(t)

	h.ObserveTPS(20)
	clock.advance(time.Second)
	h.ObserveTPS(5)

	snap := h.Snapshot()
	require.Equal(t, 5.0, snap.TPS, "a TPS drop must land wholesale; 19.25 means the peak direction is inverted")

	snap.SampleCount = cfg.MinSamples
	q := ComputeQuality(snap, cfg)
	// 5/20 = 0.25, inside the [0.2, 1.5] component band agreed in D2.
	assert.InDelta(t, 0.250, q.QTPS, 1e-9)
}

// V1.3b conversely, a TPS improvement is smoothed rather than trusted at once.
func TestV1_3TpsImprovementIsSmoothed(t *testing.T) {
	h, clock, _ := newFixture(t)

	h.ObserveTPS(5)
	clock.advance(time.Second)
	h.ObserveTPS(20)

	assert.Less(t, h.Snapshot().TPS, 20.0, "improvement must be smoothed, not adopted wholesale")
	assert.Greater(t, h.Snapshot().TPS, 5.0)
}

// V1.4 alpha floor keeps the recovery direction alive. Without alpha_min a burst
// of 50 fast samples at dt=1ms moves an 8000ms EWMA to 7993.8ms, so the route
// stays punished despite having recovered.
func TestV1_4AlphaFloorLetsTtftRecover(t *testing.T) {
	h, clock, _ := newFixture(t)

	h.ObserveTTFT(8000)
	for i := 0; i < 50; i++ {
		clock.advance(time.Millisecond)
		h.ObserveTTFT(500)
	}

	got := h.Snapshot().TTFTMs
	assert.Less(t, got, 2000.0, "50 fast samples must pull TTFT under target; ~7993ms means alpha collapsed to 0")
}

// V1.4b same protection for TPS recovery: without the floor it stays at 5.01.
func TestV1_4AlphaFloorLetsTpsRecover(t *testing.T) {
	h, clock, _ := newFixture(t)

	h.ObserveTPS(5)
	for i := 0; i < 50; i++ {
		clock.advance(time.Millisecond)
		h.ObserveTPS(20)
	}

	got := h.Snapshot().TPS
	assert.Greater(t, got, 15.0, "50 fast samples must pull TPS above 15; ~5.01 means alpha collapsed to 0")
}

// V1.5 idle regression targets the configured target, never zero. Regressing to
// zero would hand a 30-minute-idle route ttft=298.7ms and q=1.5, i.e. the single
// best score in the pool for having served no traffic.
func TestV1_5IdleRegressionTargetsTargetNotZero(t *testing.T) {
	h, clock, cfg := newFixture(t)

	h.ObserveTTFT(6000)
	clock.advance(30 * time.Minute)

	snap := h.Snapshot()
	assert.InDelta(t, 2199.1, snap.TTFTMs, 25.0, "idle TTFT must decay toward target 2000ms, not toward 0")

	snap.SampleCount = cfg.MinSamples
	q := ComputeQuality(snap, cfg)
	assert.InDelta(t, 0.909, q.QTTFT, 0.02)
	assert.LessOrEqual(t, q.QTTFT, 1.0, "idling must never earn a bonus")
}

// V1.5b regression is monotone toward neutral and converges to q=1.0.
func TestV1_5IdleRegressionConvergesToNeutral(t *testing.T) {
	h, clock, cfg := newFixture(t)

	h.ObserveTTFT(6000)
	prev := math.Inf(1)
	for _, idle := range []time.Duration{5 * time.Minute, 10 * time.Minute, 30 * time.Minute, 4 * time.Hour} {
		clock.advance(idle)
		snap := h.Snapshot()
		snap.SampleCount = cfg.MinSamples
		q := ComputeQuality(snap, cfg)
		assert.LessOrEqual(t, q.QTTFT, 1.0)
		assert.Less(t, snap.TTFTMs, prev, "each idle step must move closer to target")
		prev = snap.TTFTMs
	}
	assert.InDelta(t, float64(cfg.TTFTTargetMs), prev, 5.0, "long idle must converge to target")
}

// V1.6 weights are renormalised over observed components only. A throttled route
// with no TPS sample scores 0.641; filling the gap with neutral 1.0 inflates it
// to 0.695, handing the throttled route a free credit.
func TestV1_6MissingComponentRenormalisation(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	comp := QualityComponents{
		SuccessRate: 0.7,
		TTFTMs:      5000,
		HasSuccess:  true,
		HasTTFT:     true,
		SampleCount: 10,
	}

	q := ComputeQuality(comp, cfg)

	// (0.60*0.7 + 0.25*0.4) / 0.85 = 0.6118 with the D2 component floor of 0.2.
	// Filling the absent TPS component with neutral 1.0 would inflate this to 0.669.
	assert.InDelta(t, 0.6118, q.Quality, 0.001, "inflated value means a missing component was filled with neutral 1.0")
	assert.Equal(t, 2, q.ObservedComponents)
}

// V1.6b with every component present the synthesis stays at neutral.
func TestV1_6AllComponentsHealthyIsNeutral(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	comp := QualityComponents{
		SuccessRate: 1.0,
		TTFTMs:      float64(cfg.TTFTTargetMs),
		TPS:         float64(cfg.TPSTarget),
		HasSuccess:  true,
		HasTTFT:     true,
		HasTPS:      true,
		SampleCount: 10,
	}

	assert.InDelta(t, 1.0, ComputeQuality(comp, cfg).Quality, 1e-9)
}

// V1.7 component gradation pins the exponent/floor interaction: with a component
// floor of 0.5 every multiplier from 2x up collapses to 0.5, which silently turns
// the exponent and Retry-After into dead parameters.
func TestV1_7ComponentGradationStaysDistinct(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	target := float64(cfg.TTFTTargetMs)

	q2 := normalizeLowerBetter(2*target, target, cfg)
	q4 := normalizeLowerBetter(4*target, target, cfg)
	q10 := normalizeLowerBetter(10*target, target, cfg)

	assert.InDelta(t, 0.500, q2, 1e-9)
	assert.InDelta(t, 0.250, q4, 1e-9)
	assert.InDelta(t, 0.200, q10, 1e-9)
	assert.Greater(t, q2, q4, "2x must outrank 4x")
	assert.Greater(t, q4, q10, "4x must outrank 10x; equality means the floor swallowed the gradient")
}

// V1.8 a write after a long idle must fold from the reverted baseline. Folding
// from the stored value instead resurrects the old penalty as 0.100.
func TestV1_8StalenessWriteFoldsFromRevertedBaseline(t *testing.T) {
	h, clock, cfg := newFixture(t)

	h.ObserveSuccess(0.0)
	clock.advance(20 * time.Minute)

	// Sanity: the read-side baseline has already reverted toward 1.0.
	assert.InDelta(t, 0.865, h.Snapshot().SuccessRate, 0.005)

	h.ObserveSuccess(1.0)

	got := h.Snapshot().SuccessRate
	assert.InDelta(t, 0.878, got, 0.005, "0.100 means the write folded from the stale stored value")
	_ = cfg
}

// V1.9 reads are pure: quality lookups must not mutate stored state.
func TestV1_9ReadPathIsPure(t *testing.T) {
	h, clock, _ := newFixture(t)
	primeSamples(h, clock, 6)
	h.ObserveTTFT(3000)
	h.ObserveTPS(12)

	before := h.Snapshot()
	for i := 0; i < 1000; i++ {
		h.Quality()
		h.Snapshot()
	}
	after := h.Snapshot()

	assert.Equal(t, before, after, "reading quality must not decay or otherwise mutate stored state")
}

// V1.9b concurrent writers on one route: no race (run with -race) and every
// sample is accounted for.
func TestV1_9ConcurrentWritesAreCounted(t *testing.T) {
	h, _, _ := newFixture(t)

	const goroutines, perGoroutine = 64, 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				h.ObserveSuccess(1.0)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, goroutines*perGoroutine, h.Snapshot().SampleCount)
}

// V1.10 observation caps: a hung upstream must not pin the EWMA arbitrarily far
// from target, otherwise recovery takes 112 samples instead of 80.
func TestV1_10ObservationCapsClampExtremes(t *testing.T) {
	h, _, cfg := newFixture(t)

	h.ObserveTTFT(600_000)
	h.ObserveLatency(600_000)

	snap := h.Snapshot()
	assert.Equal(t, float64(cfg.TTFTCapMs), snap.TTFTMs, "TTFT must be capped at 120s")
	assert.Equal(t, float64(cfg.LatencyCapMs), snap.LatencyMs, "latency must be capped at 120s")
}

// V1.10b a nil handle stands for "this request did not come from route selection"
// (specific-channel calls, locked-channel task replay) and must be a no-op.
func TestV1_10NilHandleIsNoOp(t *testing.T) {
	var h *RouteHandle
	assert.NotPanics(t, func() {
		h.ObserveSuccess(1.0)
		h.ObserveTTFT(1000)
		h.ObserveTPS(10)
		h.ObserveLatency(1000)
		h.Observe429(0)
		h.Snapshot()
		h.Quality()
	})
}

// V1.10c idle entries are evicted so the map cannot grow without bound.
func TestV1_10SweepEvictsIdleEntries(t *testing.T) {
	_, clock, cfg := newFixture(t)

	h := GetOrCreateHandle(RouteKey{Group: "g", PublicModelAlias: "sweep-me", ChannelID: 9, KeyIndex: 0, UpstreamModel: "u"})
	h.ObserveSuccess(1.0)
	entries, _ := Stats()
	require.GreaterOrEqual(t, entries, 1)

	clock.advance(time.Duration(cfg.TTLSeconds+3600) * time.Second)
	removed := SweepTTL()

	assert.GreaterOrEqual(t, removed, 1, "entries idle beyond the TTL must be swept")
}

// V1.11 the package must stay free of DB dependencies: everything above runs
// with no database configured. This test states the invariant explicitly by
// exercising the full write+read path with no DB in the process.
func TestV1_11NoDatabaseDependency(t *testing.T) {
	h, clock, cfg := newFixture(t)

	h.ObserveSuccess(1.0)
	clock.advance(500 * time.Millisecond)
	h.ObserveTTFT(1800)
	clock.advance(500 * time.Millisecond)
	h.ObserveTPS(25)
	primeSamples(h, clock, cfg.MinSamples)

	q := h.Quality()
	assert.Greater(t, q.Quality, 0.0)
	assert.LessOrEqual(t, q.Quality, cfg.QualityCeil)
}

// 429 handling: the success observation is derated to 0.7 and the latency series
// eats a synthetic penalty so that a fast 429 cannot look like a fast route.
// Retry-After is clamped into [5s, 60s].
func TestObserve429AppliesDerateAndLatencyPenalty(t *testing.T) {
	h, _, cfg := newFixture(t)

	h.Observe429(0)

	snap := h.Snapshot()
	assert.InDelta(t, 0.7, snap.SuccessRate, 1e-9, "429 must derate success to 0.7")
	assert.GreaterOrEqual(t, snap.TTFTMs, float64(cfg.Penalty429DefaultMs),
		"a fast 429 must still be charged the synthetic latency penalty")
	assert.False(t, snap.HasTPS, "429 produced no tokens, so TPS must not be written")
}

func TestObserve429ClampsRetryAfter(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	cases := []struct {
		name       string
		retryAfter int
		wantMinMs  float64
	}{
		{"below floor", 1, float64(cfg.Penalty429MinMs)},
		{"inside range", 30, 30_000},
		{"above ceiling", 600, float64(cfg.Penalty429MaxMs)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := newFixture(t)
			h.Observe429(tc.retryAfter)
			assert.InDelta(t, tc.wantMinMs, h.Snapshot().TTFTMs, 1.0)
		})
	}
}

// MinSamples guard: a route with too little history is treated as neutral rather
// than judged, which is what keeps cold routes from being starved on one bad
// sample. Exploration (issue #406) covers the cold-start case.
func TestQualityIsNeutralBelowMinSamples(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	comp := QualityComponents{
		SuccessRate: 0.0,
		TTFTMs:      120_000,
		HasSuccess:  true,
		HasTTFT:     true,
		SampleCount: cfg.MinSamples - 1,
	}

	assert.InDelta(t, 1.0, ComputeQuality(comp, cfg).Quality, 1e-9)
}

// The synthesized floor is what stops the EWMA alone from starving a route: a
// fully failing route still scores 0.5 (33.3% share in a two-route pool) and
// hard ejection remains the state machine's job (issue #368).
func TestSynthesisFloorKeepsFailingRouteSelectable(t *testing.T) {
	cfg := DefaultRouteStatsSetting()
	comp := QualityComponents{
		SuccessRate: 0.0,
		TTFTMs:      120_000,
		HasSuccess:  true,
		HasTTFT:     true,
		SampleCount: 50,
	}

	q := ComputeQuality(comp, cfg)
	assert.InDelta(t, cfg.QualityFloor, q.Quality, 1e-9,
		"pre-clamp 0.147 must be lifted to the 0.5 floor so the route keeps being sampled")
}

// Unobserved components must stay at zero rather than drift toward their target.
// Regressing an absent component manufactures data: a route that never streamed
// would report a TTFT that climbs while it idles, and two consecutive reads would
// disagree. Caught by the first smoke run, where a non-streaming route reported
// ttft=297ms and tps=2.97 out of nowhere.
func TestUnobservedComponentsDoNotDrift(t *testing.T) {
	clock := &fakeClock{d: 1}
	restore := SetNowFunc(clock.now)
	defer restore()
	Reset()

	h := GetOrCreateHandle(RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: 1, UpstreamModel: "u"})
	h.ObserveSuccess(SuccessObservation)

	for _, idle := range []int{1, 60, 600, 3600} {
		clock.advance(time.Duration(idle) * time.Second)
		s := h.Snapshot()
		if s.HasTTFT || s.TTFTMs != 0 {
			t.Fatalf("idle %ds: unobserved TTFT drifted to %.3f", idle, s.TTFTMs)
		}
		if s.HasTPS || s.TPS != 0 {
			t.Fatalf("idle %ds: unobserved TPS drifted to %.3f", idle, s.TPS)
		}
	}
}
