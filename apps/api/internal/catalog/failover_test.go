package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func testFailoverKey(id int) RouteKey {
	return RouteKey{ChannelId: id, Model: "model-x"}
}

func TestFailoverWaitBudgetExactNumbers(t *testing.T) {
	// Contract case: budget 30000ms. Two failed attempts of 12s each leave
	// 6000ms of headroom, so an 8s cooldown wait does NOT fit (24000+8000 >
	// 30000) while a 6s wait fits exactly at the boundary.
	f := NewFailover(FailoverPolicy{MaxAttempts: 5, TotalBudgetMs: 30_000}, nil)

	require.True(t, f.NextAttempt(), "first dial consumes a slot")
	f.ChargeFailed(12_000)
	require.True(t, f.NextAttempt(), "second dial consumes a slot")
	f.ChargeFailed(12_000)

	assert.False(t, f.OverBudget(), "24000 consumed is inside the 30000 budget")
	assert.False(t, f.WithinWaitBudget(8_000), "24000+8000 > 30000 blocks the wait")
	assert.True(t, f.WithinWaitBudget(6_000), "24000+6000 == 30000 is the inclusive boundary")
	assert.False(t, f.WithinWaitBudget(0), "a zero wait is not a wait")

	// A fit wait consumes no attempt but DOES charge the budget; crossing the
	// ceiling by the next failed charge flips OverBudget lazily.
	f.ChargeWait(6_000)
	assert.False(t, f.OverBudget(), "consumed == budget is not exceeded")
	assert.Equal(t, 2, f.Attempts(), "the wait consumed no attempt slot")

	// Success latency is never a charge: between dials nothing accumulates.
	require.True(t, f.NextAttempt(), "third dial")
	assert.Equal(t, int64(30_000), f.consumedMs, "no implicit charge between attempts")
	f.ChargeFailed(1)
	assert.True(t, f.OverBudget(), "the first charge past the ceiling trips it")
}

func TestFailoverBudgetOff(t *testing.T) {
	f := NewFailover(FailoverPolicy{MaxAttempts: 2, TotalBudgetMs: 0}, nil)

	// Budget 0 = off (mono's legacy behavior): no wait ever fits, no charge
	// ever exceeds, only the attempt cap terminates the loop.
	for range 5 {
		f.ChargeFailed(10_000)
		f.ChargeWait(10_000)
	}
	assert.False(t, f.OverBudget(), "without a budget nothing is exceeded")
	assert.True(t, f.NextAttempt())
	assert.True(t, f.NextAttempt())
	assert.False(t, f.NextAttempt(), "attempt cap still bounds dials")
	assert.True(t, f.Exhausted())
}

func TestFailoverTriedResetSemantics(t *testing.T) {
	exclude := map[RouteKey]bool{}
	f := NewFailover(FailoverPolicy{MaxAttempts: 5, TotalBudgetMs: 30_000}, exclude)

	f.MarkTried(testFailoverKey(1))
	f.MarkTried(testFailoverKey(2))
	f.MarkTried(testFailoverKey(1)) // dedup: same key, no growth
	assert.Len(t, exclude, 2)
	assert.True(t, exclude[testFailoverKey(1)])

	// ResetTried is the "tolerate the connection time" step: the caller's map
	// (selection's exclusion input) must observe the clear through the shared
	// reference — resetting a private copy would make the wait pointless.
	f.ResetTried()
	assert.Empty(t, exclude, "the owner's map is cleared in place")

	f.MarkTried(testFailoverKey(3))
	assert.True(t, exclude[testFailoverKey(3)], "exclusions resume after the reset")
}

func TestFailoverStashTransitions(t *testing.T) {
	f := NewFailover(FailoverPolicy{MaxAttempts: 3, TotalBudgetMs: 30_000}, nil)
	zero := &types.NewAPIError{}
	e1 := &types.NewAPIError{StatusCode: 401}
	e2 := &types.NewAPIError{StatusCode: 404}

	// switchable-4xx stashing: the LAST switchable failure wins.
	f.StoreSwitchable(e1)
	f.StoreSwitchable(e2)
	require.Same(t, e2, f.TakeStashed(), "the newest switchable error is the stashed one")

	// A retryable failure clears the stash: only the last attempt's failure
	// may decide the terminal error, so an older 4xx must not resurface.
	f.StoreSwitchable(e1)
	f.ClearStash()
	assert.Nil(t, f.TakeStashed())

	// nil is never stored (a missing stashed error is the "no passthrough"
	// case, not a nil pointer the loop would have to guard against).
	f.StoreSwitchable(zero)
	require.Same(t, zero, f.TakeStashed())
	f.StoreSwitchable(nil)
	assert.Nil(t, f.TakeStashed())
}
