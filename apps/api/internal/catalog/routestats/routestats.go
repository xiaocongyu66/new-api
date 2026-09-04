package routestats

import (
	"math"
	"sync"
)

// RouteKey uniquely identifies a route unit.
// Matches model.ChannelModelRoute unique index: Group, PublicModelAlias, ChannelId, KeyIndex, UpstreamModel.
type RouteKey struct {
	Group            string
	PublicModelAlias string
	ChannelID        int
	KeyIndex         int
	UpstreamModel    string
}

// RouteState holds the EWMA state for a single route unit.
// All timestamps are nanoseconds since process start (monotonic).
type RouteState struct {
	mu sync.RWMutex

	// EWMA values
	SuccessRate float64 // EWMA success rate [0, 1]
	TTFTMs      float64 // EWMA TTFT in milliseconds
	TPS         float64 // EWMA tokens per second
	LatencyMs   float64 // EWMA latency in milliseconds

	// Observation tracking. SampleCount counts attributable requests, which is
	// exactly the number of success observations: TTFT, TPS and latency are facets
	// of the same request, so counting them too would let a single request clear a
	// MinSamples threshold of 3.
	HasSuccess  bool
	HasTTFT     bool
	HasTPS      bool
	HasLatency  bool
	SampleCount int

	// Last update time (nanoseconds since process start)
	LastUpdateNs int64

	// Key for eviction/debugging
	Key RouteKey
}

// RouteHandle is an opaque handle to a RouteState.
// Hot path uses pointer dereference instead of map lookup.
type RouteHandle struct {
	state *RouteState
}

// State returns the underlying RouteState (read-only access).
// Caller must not modify the returned state directly; use Observe* methods.
func (h *RouteHandle) State() *RouteState {
	return h.state
}

// Key returns the route key.
func (h *RouteHandle) Key() RouteKey {
	return h.state.Key
}

// routeStore is the sharded map for route states.
type routeStore struct {
	shards    []*shard
	shardMask uint64
}

type shard struct {
	mu     sync.RWMutex
	states map[RouteKey]*RouteState
}

const defaultShards = 64

// Global store instance
var store = newRouteStore(defaultShards)

// newRouteStore must stay free of getConfig(): it runs during package variable
// initialisation, before the settings init() has populated the default config.
// TTL is therefore resolved per sweep instead of being cached here.
func newRouteStore(shards int) *routeStore {
	n := 1
	for n < shards {
		n <<= 1
	}
	rs := &routeStore{
		shards:    make([]*shard, n),
		shardMask: uint64(n - 1),
	}
	for i := 0; i < n; i++ {
		rs.shards[i] = &shard{states: make(map[RouteKey]*RouteState)}
	}
	return rs
}

func (rs *routeStore) getShard(key RouteKey) *shard {
	// FNV-1a over the identity fields. Unsigned arithmetic keeps the index in
	// range: a signed hash can overflow negative and panic on indexing.
	const offset64 = uint64(14695981039346656037)
	const prime64 = uint64(1099511628211)
	h := offset64
	writeString := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= prime64
		}
	}
	writeString(key.Group)
	writeString(key.PublicModelAlias)
	writeString(key.UpstreamModel)
	h ^= uint64(uint32(key.ChannelID))
	h *= prime64
	h ^= uint64(uint32(key.KeyIndex))
	h *= prime64
	return rs.shards[h&rs.shardMask]
}

// GetOrCreateHandle returns a handle for the route key, creating state if needed.
// The returned handle is valid until the entry is evicted by TTL sweep.
// Caller should not hold the handle across TTL sweeps without re-acquiring.
func GetOrCreateHandle(key RouteKey) *RouteHandle {
	shard := store.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	st, ok := shard.states[key]
	if !ok {
		st = &RouteState{
			Key:          key,
			LastUpdateNs: NowMonotonic(),
		}
		shard.states[key] = st
	}
	return &RouteHandle{state: st}
}

// GetHandle returns a handle for the route key, or nil if not found.
func GetHandle(key RouteKey) *RouteHandle {
	shard := store.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	st, ok := shard.states[key]
	if !ok {
		return nil
	}
	return &RouteHandle{state: st}
}

// ObserveSuccess records a success/fatal/throttled outcome for success rate EWMA.
// x: 1.0 = success, 0.7 = throttled, 0.0 = fatal.
// Neutral outcomes are NOT recorded (caller should not call for neutral).
// This implements per-attempt attribution (D1).
func (h *RouteHandle) ObserveSuccess(x float64) {
	if h == nil || h.state == nil {
		return
	}
	cfg := getConfig()
	if !cfg.Enabled {
		return
	}

	st := h.state
	st.mu.Lock()
	defer st.mu.Unlock()

	now := NowMonotonic()
	deltaNs := now - st.LastUpdateNs
	if deltaNs < 0 {
		deltaNs = 0
	}
	deltaSec := float64(deltaNs) / 1e9

	// Apply staleness regression before updating
	if deltaSec > 0 && (st.HasSuccess || st.HasTTFT || st.HasTPS || st.HasLatency) {
		reg := RegressionComponents(QualityComponents{
			SuccessRate: st.SuccessRate,
			TTFTMs:      st.TTFTMs,
			TPS:         st.TPS,
			LatencyMs:   st.LatencyMs,
			HasSuccess:  st.HasSuccess,
			HasTTFT:     st.HasTTFT,
			HasTPS:      st.HasTPS,
			HasLatency:  st.HasLatency,
			SampleCount: st.SampleCount,
		}, deltaSec, cfg)
		st.SuccessRate = reg.SuccessRate
		st.TTFTMs = reg.TTFTMs
		st.TPS = reg.TPS
		st.LatencyMs = reg.LatencyMs
	}

	// EWMA update for success rate
	alpha := cfg.AlphaSuccess
	if !st.HasSuccess {
		st.SuccessRate = x
		st.HasSuccess = true
	} else {
		st.SuccessRate = st.SuccessRate + alpha*(x-st.SuccessRate)
	}
	st.SampleCount++
	st.LastUpdateNs = now
}

// ObserveTTFT records a TTFT observation in milliseconds.
// Observations > TTFTCapMs are clamped to the cap (D3).
// Peak-sensitive: if obs > cur, cur = obs (full drop-in); else EWMA.
func (h *RouteHandle) ObserveTTFT(ttftMs float64) {
	if h == nil || h.state == nil {
		return
	}
	cfg := getConfig()
	if !cfg.Enabled {
		return
	}

	// Cap observation
	if ttftMs > float64(cfg.TTFTCapMs) {
		ttftMs = float64(cfg.TTFTCapMs)
	}

	st := h.state
	st.mu.Lock()
	defer st.mu.Unlock()

	now := NowMonotonic()
	deltaNs := now - st.LastUpdateNs
	if deltaNs < 0 {
		deltaNs = 0
	}
	deltaSec := float64(deltaNs) / 1e9

	// Apply staleness regression
	if deltaSec > 0 && (st.HasSuccess || st.HasTTFT || st.HasTPS || st.HasLatency) {
		reg := RegressionComponents(QualityComponents{
			SuccessRate: st.SuccessRate,
			TTFTMs:      st.TTFTMs,
			TPS:         st.TPS,
			LatencyMs:   st.LatencyMs,
			HasSuccess:  st.HasSuccess,
			HasTTFT:     st.HasTTFT,
			HasTPS:      st.HasTPS,
			HasLatency:  st.HasLatency,
			SampleCount: st.SampleCount,
		}, deltaSec, cfg)
		st.SuccessRate = reg.SuccessRate
		st.TTFTMs = reg.TTFTMs
		st.TPS = reg.TPS
		st.LatencyMs = reg.LatencyMs
	}

	// Peak-sensitive EWMA: bad news (larger) drops in fully
	alphaEff := effectiveAlpha(deltaSec, cfg)
	if !st.HasTTFT {
		st.TTFTMs = ttftMs
		st.HasTTFT = true
	} else if ttftMs > st.TTFTMs {
		st.TTFTMs = ttftMs // Peak drop-in
	} else {
		st.TTFTMs = st.TTFTMs + alphaEff*(ttftMs-st.TTFTMs)
	}
	st.LastUpdateNs = now
}

// ObserveTPS records a TPS observation in tokens/second.
// Peak-sensitive: if obs < cur, cur = obs (full drop-in); else EWMA.
func (h *RouteHandle) ObserveTPS(tps float64) {
	if h == nil || h.state == nil {
		return
	}
	cfg := getConfig()
	if !cfg.Enabled {
		return
	}

	st := h.state
	st.mu.Lock()
	defer st.mu.Unlock()

	now := NowMonotonic()
	deltaNs := now - st.LastUpdateNs
	if deltaNs < 0 {
		deltaNs = 0
	}
	deltaSec := float64(deltaNs) / 1e9

	// Apply staleness regression
	if deltaSec > 0 && (st.HasSuccess || st.HasTTFT || st.HasTPS || st.HasLatency) {
		reg := RegressionComponents(QualityComponents{
			SuccessRate: st.SuccessRate,
			TTFTMs:      st.TTFTMs,
			TPS:         st.TPS,
			LatencyMs:   st.LatencyMs,
			HasSuccess:  st.HasSuccess,
			HasTTFT:     st.HasTTFT,
			HasTPS:      st.HasTPS,
			HasLatency:  st.HasLatency,
			SampleCount: st.SampleCount,
		}, deltaSec, cfg)
		st.SuccessRate = reg.SuccessRate
		st.TTFTMs = reg.TTFTMs
		st.TPS = reg.TPS
		st.LatencyMs = reg.LatencyMs
	}

	// Peak-sensitive EWMA: bad news (smaller) drops in fully
	alphaEff := effectiveAlpha(deltaSec, cfg)
	if !st.HasTPS {
		st.TPS = tps
		st.HasTPS = true
	} else if tps < st.TPS {
		st.TPS = tps // Peak drop-in for TPS (lower is worse)
	} else {
		st.TPS = st.TPS + alphaEff*(tps-st.TPS)
	}
	st.LastUpdateNs = now
}

// ObserveLatency records a latency observation in milliseconds.
// Observations > LatencyCapMs are clamped to the cap (D3).
// Peak-sensitive: if obs > cur, cur = obs (full drop-in); else EWMA.
func (h *RouteHandle) ObserveLatency(latencyMs float64) {
	if h == nil || h.state == nil {
		return
	}
	cfg := getConfig()
	if !cfg.Enabled {
		return
	}

	// Cap observation
	if latencyMs > float64(cfg.LatencyCapMs) {
		latencyMs = float64(cfg.LatencyCapMs)
	}

	st := h.state
	st.mu.Lock()
	defer st.mu.Unlock()

	now := NowMonotonic()
	deltaNs := now - st.LastUpdateNs
	if deltaNs < 0 {
		deltaNs = 0
	}
	deltaSec := float64(deltaNs) / 1e9

	// Apply staleness regression
	if deltaSec > 0 && (st.HasSuccess || st.HasTTFT || st.HasTPS || st.HasLatency) {
		reg := RegressionComponents(QualityComponents{
			SuccessRate: st.SuccessRate,
			TTFTMs:      st.TTFTMs,
			TPS:         st.TPS,
			LatencyMs:   st.LatencyMs,
			HasSuccess:  st.HasSuccess,
			HasTTFT:     st.HasTTFT,
			HasTPS:      st.HasTPS,
			HasLatency:  st.HasLatency,
			SampleCount: st.SampleCount,
		}, deltaSec, cfg)
		st.SuccessRate = reg.SuccessRate
		st.TTFTMs = reg.TTFTMs
		st.TPS = reg.TPS
		st.LatencyMs = reg.LatencyMs
	}

	// Peak-sensitive EWMA: bad news (larger) drops in fully
	alphaEff := effectiveAlpha(deltaSec, cfg)
	if !st.HasLatency {
		st.LatencyMs = latencyMs
		st.HasLatency = true
	} else if latencyMs > st.LatencyMs {
		st.LatencyMs = latencyMs // Peak drop-in
	} else {
		st.LatencyMs = st.LatencyMs + alphaEff*(latencyMs-st.LatencyMs)
	}
	st.LastUpdateNs = now
}

// MinTPSTokens is the smallest completion-token count that yields a usable
// tokens-per-second sample. Very short answers are dominated by fixed overhead,
// so their apparent throughput says more about the prompt than the route.
const MinTPSTokens = 16

// Observation values fed into the success EWMA. Throttled sits between success
// and fatal because a 429 means "busy", not "broken": a full 0.0 would collapse
// a merely rate-limited route to the floor and starve it.
const (
	SuccessObservation   = 1.0
	ThrottledObservation = 0.7
	FatalObservation     = 0.0
)

// Observe429 records a throttled outcome. Three things happen, and all three are
// deliberate:
//
//   - success is derated to throttledObservation (0.7), not 0.0: a 429 means the
//     route is busy, not broken, so it must not be treated as a hard failure.
//   - the synthetic penalty is fed into the TTFT series, because TTFT is what
//     participates in the quality synthesis. A 429 answers very fast, so without
//     this penalty a throttled route would look like the fastest route in the
//     pool and the balancer would send it MORE traffic (the Linkerd load-biaser
//     failure mode).
//   - TPS is deliberately not written: no tokens were generated, and inventing a
//     TPS sample here would punish the route twice for one event.
//
// retryAfterSec > 0 clamps the penalty into [Penalty429MinMs, Penalty429MaxMs];
// otherwise Penalty429DefaultMs applies. Note the component floor saturates the
// TTFT score at target/floor, so penalties beyond that point are indistinguishable.
func (h *RouteHandle) Observe429(retryAfterSec int) {
	if h == nil || h.state == nil {
		return
	}
	cfg := getConfig()
	if !cfg.Enabled {
		return
	}

	penaltyMs := float64(cfg.Penalty429DefaultMs)
	if retryAfterSec > 0 {
		penaltyMs = float64(retryAfterSec) * 1000
		if penaltyMs < float64(cfg.Penalty429MinMs) {
			penaltyMs = float64(cfg.Penalty429MinMs)
		}
		if penaltyMs > float64(cfg.Penalty429MaxMs) {
			penaltyMs = float64(cfg.Penalty429MaxMs)
		}
	}

	h.ObserveTTFT(penaltyMs)
	h.ObserveSuccess(ThrottledObservation)
}

// effectiveAlpha computes α_eff = max(α_min, 1 - exp(-Δt/τ)).
// D4: Δt* = 3.08s is where α_eff crosses α_min=0.05 with τ=60s.
// Latency half-life of 41.6s ONLY holds when request rate < 0.32 req/s (Δt > 3.08s).
func effectiveAlpha(deltaSec float64, cfg *RouteStatsSetting) float64 {
	if deltaSec <= 0 {
		return cfg.AlphaMin
	}
	alpha := 1.0 - exp(-deltaSec/cfg.Tau)
	if alpha < cfg.AlphaMin {
		return cfg.AlphaMin
	}
	if alpha > 1.0 {
		return 1.0
	}
	return alpha
}

// exp is a wrapper for math.Exp to allow test mocking if needed.
var exp = math.Exp

// Snapshot returns a copy of the current state with staleness regression applied.
// This is a pure read: it does NOT modify the stored state.
// The returned components reflect the regressed values as of now.
func (h *RouteHandle) Snapshot() QualityComponents {
	if h == nil || h.state == nil {
		return QualityComponents{}
	}
	cfg := getConfig()

	st := h.state
	st.mu.RLock()
	defer st.mu.RUnlock()

	now := NowMonotonic()
	deltaNs := now - st.LastUpdateNs
	if deltaNs < 0 {
		deltaNs = 0
	}
	deltaSec := float64(deltaNs) / 1e9

	comp := QualityComponents{
		SuccessRate: st.SuccessRate,
		TTFTMs:      st.TTFTMs,
		TPS:         st.TPS,
		LatencyMs:   st.LatencyMs,
		HasSuccess:  st.HasSuccess,
		HasTTFT:     st.HasTTFT,
		HasTPS:      st.HasTPS,
		HasLatency:  st.HasLatency,
		SampleCount: st.SampleCount,
	}

	if deltaSec > 0 {
		comp = RegressionComponents(comp, deltaSec, cfg)
	}

	return comp
}

// Quality returns the synthesized quality score for this route.
// This is a pure read: it does NOT modify the stored state.
func (h *RouteHandle) Quality() QualityResult {
	comp := h.Snapshot()
	return ComputeQuality(comp, getConfig())
}

// SweepTTL removes entries older than TTL.
// Returns the number of entries removed.
func SweepTTL() int {
	cfg := getConfig()
	ttlNs := int64(cfg.TTLSeconds) * 1e9
	if ttlNs <= 0 {
		return 0
	}
	now := NowMonotonic()
	removed := 0

	for _, shard := range store.shards {
		shard.mu.Lock()
		for key, st := range shard.states {
			if now-st.LastUpdateNs > ttlNs {
				delete(shard.states, key)
				removed++
			}
		}
		shard.mu.Unlock()
	}
	return removed
}

// Stats returns aggregate statistics for monitoring.
func Stats() (totalEntries int, totalSamples int64) {
	for _, shard := range store.shards {
		shard.mu.RLock()
		totalEntries += len(shard.states)
		for _, st := range shard.states {
			totalSamples += int64(st.SampleCount)
		}
		shard.mu.RUnlock()
	}
	return
}

// Reset clears all route state. For testing and kill-switch toggles.
func Reset() {
	for _, shard := range store.shards {
		shard.mu.Lock()
		shard.states = make(map[RouteKey]*RouteState)
		shard.mu.Unlock()
	}
}
