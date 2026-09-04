package routestats

import (
	"math"
	"sync"
)

// PoolKey identifies one competing pool of route units. Selection happens within
// a (group, alias) pair, so the share window must be scoped the same way: two
// groups serving the same alias are separate pools with separate expected shares.
type PoolKey struct {
	Group            string
	PublicModelAlias string
}

// RouteID identifies one route unit inside a pool. It is the route row's own
// identity minus the pool fields, which is what selection picks.
type RouteID struct {
	ChannelID     int
	KeyIndex      int
	UpstreamModel string
}

// shareEntry is one recorded selection. It stores the target shares that were in
// force at the time, because the correction compares against the average target
// over the route's own opportunities, not against the target of the current
// request. Recomputing from the current base scores instead fails the re-entry
// gate: a route returning after an absence reads as starving and gets boosted
// (58.4% of the next 100 requests against a fair 50%).
type shareEntry struct {
	selected RouteID
	// targets holds the base-score share of every candidate that competed in this
	// request. Its length is the pool size at that moment, which is why a route
	// that was not a candidate contributes nothing to its own denominator.
	targets map[RouteID]float64
}

// routeShare accumulates one route's presence in the window.
type routeShare struct {
	selections    int
	opportunities int
	targetSum     float64
}

// poolWindow is a fixed-capacity ring of recent selections for one pool.
type poolWindow struct {
	mu      sync.Mutex
	entries []shareEntry
	head    int
	size    int
	routes  map[RouteID]*routeShare
}

// shareStore holds one window per pool.
var shareStore struct {
	mu    sync.RWMutex
	pools map[PoolKey]*poolWindow
}

func init() {
	shareStore.pools = make(map[PoolKey]*poolWindow)
}

// ShareCorrection is the diagnostic breakdown for one route unit, so an operator
// can recompute the final score by hand from an admin query.
type ShareCorrection struct {
	// ExpectedShare is the average base-score share this route was entitled to
	// over the requests where it actually competed.
	ExpectedShare float64
	// ActualShare is selections divided by opportunities, not by window length.
	ActualShare float64
	// Correction is the clamped multiplier applied to the base score.
	Correction float64
	// Opportunities is how many windowed requests had this route as a candidate.
	Opportunities int
	// Selections is how many of those picked it.
	Selections int
}

// neutralCorrection is what a route with no window history gets: no boost, no
// derate. A route that has never competed is not behind on its share.
func neutralCorrection() ShareCorrection {
	return ShareCorrection{Correction: 1.0}
}

// getPool returns the window for a pool, creating it on demand.
func getPool(pool PoolKey, capacity int) *poolWindow {
	shareStore.mu.RLock()
	w := shareStore.pools[pool]
	shareStore.mu.RUnlock()
	if w != nil {
		return w
	}
	shareStore.mu.Lock()
	defer shareStore.mu.Unlock()
	if w = shareStore.pools[pool]; w != nil {
		return w
	}
	w = &poolWindow{
		entries: make([]shareEntry, 0, capacity),
		routes:  make(map[RouteID]*routeShare),
	}
	shareStore.pools[pool] = w
	return w
}

// Corrections returns the share-deficit multiplier for every route in targets.
//
// targets maps each candidate to its base-score share (static x quality x health,
// normalised over the candidate set). The correction pulls an over-served route
// down and an under-served route up, but only towards the share its own base
// score already earns it: at convergence every correction is exactly 1.0, so the
// steady state is the base-score distribution and not something the window
// invented.
//
// A window size of zero disables correction entirely and every route gets 1.0.
// That is the A/B baseline: one implementation, one switch, no second code path.
func Corrections(pool PoolKey, targets map[RouteID]float64, cfg *RouteStatsSetting) map[RouteID]ShareCorrection {
	out := make(map[RouteID]ShareCorrection, len(targets))
	if cfg == nil || !cfg.Enabled || cfg.ShareWindowSize <= 0 {
		for id := range targets {
			out[id] = neutralCorrection()
		}
		return out
	}

	w := getPool(pool, cfg.ShareWindowSize)
	w.mu.Lock()
	defer w.mu.Unlock()

	for id := range targets {
		rs := w.routes[id]
		if rs == nil || rs.opportunities == 0 {
			out[id] = neutralCorrection()
			continue
		}
		actual := float64(rs.selections) / float64(rs.opportunities)
		expected := rs.targetSum / float64(rs.opportunities)
		corr := expected / (actual + cfg.ShareEpsilon)
		if math.IsNaN(corr) || math.IsInf(corr, 0) {
			corr = 1.0
		}
		out[id] = ShareCorrection{
			ExpectedShare: expected,
			ActualShare:   actual,
			Correction:    clamp(corr, cfg.ShareCorrMin, cfg.ShareCorrMax),
			Opportunities: rs.opportunities,
			Selections:    rs.selections,
		}
	}
	return out
}

// RecordSelection appends one selection to the pool window, evicting the oldest
// entry once the window is full.
//
// Every path that serves a route unit must call this, including the ones that
// bypass weighted random selection (channel affinity, specific channel, locked
// replay). Those paths are the reason the window exists: the final score cannot
// influence traffic it never sees, so unless their requests land in the window
// the correction has nothing to correct. Leaving them out measurably degrades
// the balancer back to the no-correction baseline.
//
// targets must be the same map handed to Corrections for this request, so the
// recorded entitlement matches the one the decision was made against.
func RecordSelection(pool PoolKey, selected RouteID, targets map[RouteID]float64, cfg *RouteStatsSetting) {
	if cfg == nil || !cfg.Enabled || cfg.ShareWindowSize <= 0 || len(targets) == 0 {
		return
	}

	// Copy the target snapshot: the caller may reuse or mutate its map, and this
	// entry outlives the request by up to a full window.
	snapshot := make(map[RouteID]float64, len(targets))
	for id, share := range targets {
		snapshot[id] = share
	}

	w := getPool(pool, cfg.ShareWindowSize)
	w.mu.Lock()
	defer w.mu.Unlock()

	// The admin can resize the window at runtime, and both directions break the
	// ring: shrinking leaves entries past the new capacity, growing appends at the
	// tail while head is mid-slice, so the oldest entry is no longer at head and
	// eviction stops being FIFO. Rebuild in logical order once, then the fill and
	// overwrite paths below hold their invariant (head is only ever non-zero on a
	// full ring).
	if len(w.entries) != cfg.ShareWindowSize {
		w.reorderLocked(cfg.ShareWindowSize)
	}
	if w.size >= cfg.ShareWindowSize {
		w.evictOldestLocked()
	}

	entry := shareEntry{selected: selected, targets: snapshot}
	if len(w.entries) < cfg.ShareWindowSize {
		w.entries = append(w.entries, entry)
	} else {
		w.entries[(w.head+w.size)%len(w.entries)] = entry
	}
	w.size++
	w.applyLocked(entry, 1)
}

// evictOldestLocked removes the entry at the head. Callers must hold w.mu.
func (w *poolWindow) evictOldestLocked() {
	if w.size == 0 {
		return
	}
	old := w.entries[w.head]
	w.applyLocked(old, -1)
	w.entries[w.head] = shareEntry{}
	w.head = (w.head + 1) % len(w.entries)
	w.size--
}

// reorderLocked rebuilds the ring in logical order for a new capacity, dropping
// the oldest entries that no longer fit and unfolding their counters. After it
// returns, head is 0 and entries holds size items oldest-first, so the caller's
// append path fills towards capacity and the overwrite path stays FIFO.
// Callers must hold w.mu.
func (w *poolWindow) reorderLocked(capacity int) {
	if capacity <= 0 {
		return
	}
	for w.size > capacity {
		w.evictOldestLocked()
	}
	ordered := make([]shareEntry, 0, capacity)
	for i := range w.size {
		ordered = append(ordered, w.entries[(w.head+i)%len(w.entries)])
	}
	w.entries = ordered
	w.head = 0
}

// applyLocked folds one entry into the per-route counters, or unfolds it when
// sign is -1. Counters are maintained incrementally so a correction lookup costs
// a map read per candidate instead of a walk over the whole window.
// Callers must hold w.mu.
func (w *poolWindow) applyLocked(entry shareEntry, sign int) {
	for id, share := range entry.targets {
		rs := w.routes[id]
		if rs == nil {
			if sign < 0 {
				continue
			}
			rs = &routeShare{}
			w.routes[id] = rs
		}
		rs.opportunities += sign
		rs.targetSum += share * float64(sign)
		if id == entry.selected {
			rs.selections += sign
		}
		// Drop routes that have aged out of the window entirely, so a pool that
		// churns through route units does not accumulate dead counters.
		if rs.opportunities <= 0 {
			delete(w.routes, id)
		}
	}
}

// ResetShares clears every pool window. For tests and kill-switch toggles.
func ResetShares() {
	shareStore.mu.Lock()
	shareStore.pools = make(map[PoolKey]*poolWindow)
	shareStore.mu.Unlock()
}

// SweepSharePools drops windows whose pool no longer receives traffic, keyed by
// the pools still present in keep. Returns the number of pools removed.
//
// A nil keep is "unknown", not "empty", and sweeps nothing. The caller cannot
// always enumerate the live pools (see catalog.GetActiveRouteStatsPoolKeys), and
// treating that as an empty keep set would discard every pool's
// share-correction history — the opposite of evicting the orphans.
func SweepSharePools(keep map[PoolKey]struct{}) int {
	if keep == nil {
		return 0
	}
	shareStore.mu.Lock()
	defer shareStore.mu.Unlock()
	removed := 0
	for pool := range shareStore.pools {
		if _, ok := keep[pool]; !ok {
			delete(shareStore.pools, pool)
			removed++
		}
	}
	return removed
}

// SharePoolCount reports how many pool windows are live, for monitoring.
func SharePoolCount() int {
	shareStore.mu.RLock()
	defer shareStore.mu.RUnlock()
	return len(shareStore.pools)
}
