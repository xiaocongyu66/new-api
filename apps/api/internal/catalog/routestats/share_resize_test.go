package routestats

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShareWindowGrowthKeepsFifoOrder covers the other half of W3.1's rolling
// window boundary: the admin can raise ShareWindowSize at runtime, and the ring
// has to survive it.
//
// Growing is the harder direction. Shrinking evicts down to the new capacity and
// head lands wherever it lands; growing used to append at the tail while head sat
// mid-slice, so the slot at head was no longer the oldest entry and eviction
// stopped being FIFO. The counters stayed plausible — totals still matched the
// window — which is exactly why this needs an order assertion rather than a sum.
func TestShareWindowGrowthKeepsFifoOrder(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	a, b := routeID(1), routeID(2)
	targets := map[RouteID]float64{a: 0.5, b: 0.5}

	small := shareTestSetting(4)
	// Fill past capacity so head is mid-slice when the resize lands.
	for range 6 {
		RecordSelection(pool, a, targets, small)
	}
	w := getPool(pool, 4)
	w.mu.Lock()
	require.NotZero(t, w.head, "fixture must leave head mid-slice for the resize to matter")
	w.mu.Unlock()

	// Grow, then push exactly one window's worth of a different winner through.
	big := shareTestSetting(8)
	for range 8 {
		RecordSelection(pool, b, targets, big)
	}

	corr := Corrections(pool, targets, big)
	// FIFO means the 8 newest entries are the only ones left, and all of them
	// picked b. A broken ring keeps a stale entry for a and evicts a fresh one.
	assert.Equal(t, 8, corr[b].Selections, "every entry in the grown window selected b")
	assert.Equal(t, 8, corr[b].Opportunities)
	assert.Zero(t, corr[a].Selections, "no pre-resize selection may survive a full window of new traffic")
	assert.Equal(t, 8, corr[a].Opportunities, "a was a candidate on every recorded request")

	w.mu.Lock()
	defer w.mu.Unlock()
	assert.Equal(t, 8, w.size, "window must hold exactly the new capacity")
	assert.Len(t, w.entries, 8, "backing slice must match capacity after resize")
	for i := range w.size {
		assert.Equal(t, b, w.entries[(w.head+i)%len(w.entries)].selected,
			"entry %d must be one of the post-resize selections", i)
	}
}

// TestShareWindowResizeRoundTripStaysBounded pins that repeated resizes cannot
// inflate the window or leave negative counters behind. Each resize rebuilds the
// ring, and a rebuild that miscounts shows up here as opportunities exceeding the
// window it was measured over.
func TestShareWindowResizeRoundTripStaysBounded(t *testing.T) {
	ResetShares()
	t.Cleanup(ResetShares)

	pool := PoolKey{Group: "g", PublicModelAlias: "m"}
	base := map[RouteID]float64{routeID(1): 1, routeID(2): 1}
	avail := []RouteID{routeID(1), routeID(2)}
	rng := rand.New(rand.NewPCG(0xF1F0, 3))

	for _, window := range []int{10, 40, 15, 40, 5} {
		cfg := shareTestSetting(window)
		for range 30 {
			drawPool(t, pool, base, avail, rng, cfg)
		}

		w := getPool(pool, window)
		w.mu.Lock()
		size := w.size
		entries := len(w.entries)
		w.mu.Unlock()

		require.LessOrEqual(t, size, window, "window %d overfilled to %d", window, size)
		require.LessOrEqual(t, entries, window, "backing slice %d exceeds window %d", entries, window)

		corr := Corrections(pool, map[RouteID]float64{routeID(1): 0.5, routeID(2): 0.5}, cfg)
		total := 0
		for id, c := range corr {
			require.GreaterOrEqual(t, c.Selections, 0, "route %d went negative", id.ChannelID)
			require.LessOrEqual(t, c.Opportunities, size,
				"route %d claims %d opportunities in a %d-entry window", id.ChannelID, c.Opportunities, size)
			total += c.Selections
		}
		assert.Equal(t, size, total, "one winner per recorded request at window %d", window)
	}
}
