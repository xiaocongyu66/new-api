package channel

import (
	"github.com/QuantumNous/new-api/relaykit/types"
)

// FailoverPolicy is the request-level failover budget (mono's RetryPolicy,
// port of crates/gateway/dispatch/src/retry.rs). MaxAttempts counts dials
// INCLUDING the first attempt (mono gateway.retry.max_attempts);
// TotalBudgetMs is the time budget: it accumulates ONLY failed-attempt
// latency and cooldown-recovery waits — a successful attempt's latency is
// never charged, and waiting consumes no attempt slot. 0 = time budget
// off (attempt cap only, mono's legacy behavior).
type FailoverPolicy struct {
	MaxAttempts   int
	TotalBudgetMs int64
}

// Failover is the request-lifetime failover state machine (mono's
// Failover): the tried-exclusion set, the attempt counter, the time-budget
// accumulator and the last switchable-4xx stash. It owns the caller's
// exclusion map by reference (the relay loop's SelectParams.ExcludeRoutes),
// so MarkTried/ResetTried move exactly the map selection consults — the
// mono FailedAccountIDs equivalent keyed by route unit.
//
// All methods are pure state transitions; the relay loop drives them.
type Failover struct {
	policy     FailoverPolicy
	exclude    map[RouteKey]bool
	attempts   int
	consumedMs int64
	// stashed is the most recent channel-scoped 4xx (FatalButSwitchable):
	// when the budgets run out it is passed through to the client verbatim
	// instead of being disguised as a 503/504. A later retryable or
	// throttled failure clears it (only the LAST attempt's failure decides
	// the terminal error).
	stashed *types.NewAPIError
}

// NewFailover builds a failover state over the caller's exclusion map. A
// nil exclude map is adopted as a fresh one; a non-nil one is shared
// (mutations are visible to the owner).
func NewFailover(policy FailoverPolicy, exclude map[RouteKey]bool) *Failover {
	if exclude == nil {
		exclude = make(map[RouteKey]bool)
	}
	return &Failover{policy: policy, exclude: exclude}
}

// Exclude is the tried set, borrowed by selection as its exclusion map.
func (f *Failover) Exclude() map[RouteKey]bool {
	return f.exclude
}

// NextAttempt is true only for a dial that will actually reach the
// upstream; waiting and candidate switching never consume a slot (mono:
// the attempt counter increments at select-success, not at loop-top).
// False once the MaxAttempts cap is reached.
func (f *Failover) NextAttempt() bool {
	if f.attempts >= f.policy.MaxAttempts {
		return false
	}
	f.attempts++
	return true
}

// Attempts is the number of upstream dials so far.
func (f *Failover) Attempts() int {
	return f.attempts
}

// MarkTried records a failed route unit in the exclusion set (deduped,
// like the map assignment the relay loop used to do directly).
func (f *Failover) MarkTried(key RouteKey) {
	f.exclude[key] = true
}

// ResetTried clears the exclusion set — mono's "tolerate the connection
// time" step after a cooldown wait: otherwise the recovered candidates
// would still be blocked by the exclude set and the wait would be
// pointless. Bounded by the attempt cap and the time budget.
func (f *Failover) ResetTried() {
	clear(f.exclude)
}

// ChargeFailed adds a failed attempt's upstream latency to the time
// budget (mono S2: failures are the only non-wait charge).
func (f *Failover) ChargeFailed(ms int64) {
	if ms > 0 {
		f.consumedMs += ms
	}
}

// ChargeWait adds a cooldown-recovery wait to the time budget. A wait
// itself consumes no attempt.
func (f *Failover) ChargeWait(ms int64) {
	if ms > 0 {
		f.consumedMs += ms
	}
}

// WithinWaitBudget reports whether a wait of waitMs fits the time budget
// (mono's wait_ok: retry_after>0 && budget>0 && consumed+wait<=budget).
// With the budget off (TotalBudgetMs=0) nothing fits: the gateway must
// not block indefinitely waiting out a cooldown.
func (f *Failover) WithinWaitBudget(waitMs int64) bool {
	if f.policy.TotalBudgetMs <= 0 || waitMs <= 0 {
		return false
	}
	return f.consumedMs+waitMs <= f.policy.TotalBudgetMs
}

// OverBudget reports whether the time budget was exceeded by failed
// charges (mono: the loop terminates at the first charge that crosses
// it, lazily — the check happens at charge sites, not per attempt).
func (f *Failover) OverBudget() bool {
	return f.policy.TotalBudgetMs > 0 && f.consumedMs > f.policy.TotalBudgetMs
}

// Exhausted reports whether the attempt cap is reached (mono's
// next_attempt → None).
func (f *Failover) Exhausted() bool {
	return f.attempts >= f.policy.MaxAttempts
}

// StoreSwitchable stashes a channel-scoped 4xx (401/403/404) for
// terminal passthrough; the LAST switchable failure wins.
func (f *Failover) StoreSwitchable(err *types.NewAPIError) {
	if err != nil {
		f.stashed = err
	}
}

// ClearStash drops a stashed 4xx — a later retryable or throttled
// failure is the terminal error's authority, not an older 4xx.
func (f *Failover) ClearStash() {
	f.stashed = nil
}

// TakeStashed returns (and consumes) the stashed switchable 4xx, or nil
// when there is none.
func (f *Failover) TakeStashed() *types.NewAPIError {
	stashed := f.stashed
	f.stashed = nil
	return stashed
}
