package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// relayAttemptKind is the failover loop's classification of one failed
// relay attempt — the port of mono's AttemptOutcome: it encodes what the
// failure implies about the candidate pool, not whether a retry is allowed.
type relayAttemptKind int

const (
	relayAttemptDone            relayAttemptKind = iota // no failure (success arm of the enum)
	relayAttemptRetryable                               // 5xx/408/transport failure: this candidate is dead right now, switch
	relayAttemptThrottled                               // 429: the channel is busy, not broken — switch candidates, never sleep in-loop (mono switches on 429; the client-side backoff is the Retry-After hint the terminal error carries)
	relayAttemptFatalSwitchable                         // channel-scoped 4xx (401/403/404): this candidate is rejected upstream, another may serve; stash for terminal passthrough
	relayAttemptFatal                                   // caller-side failure (400/413/422, canceled request): no candidate switch can help
)

// classifyAttempt is mono's egress status + failure-scope classification: it
// answers "what happened". The admin-configurable AutomaticRetryStatusCodes
// range policy answers "may we retry", and the loop ANDs the two — but the
// kind wins where mono's FailureScope says switching cannot help: a
// relayAttemptFatal failure stops the loop even when the range allows it.
//
// Behavior delta vs the pre-port range-only loop (faithful to mono): upstream
// 4xx outside {401, 403, 404, 408, 429} (e.g. 409/410/425) used to switch
// candidates under the default range; mono treats every non-channel-scoped
// 4xx as Request-scope — the same request fails on every candidate — so they
// are now terminal and pass through to the client.

// Range authority is preserved for the switchable kinds: 5xx/408/429 and the
// channel-scoped 4xx still switch only when the admin range allows the code.
func classifyAttempt(err *types.NewAPIError) relayAttemptKind {
	if err == nil {
		return relayAttemptDone
	}
	// A canceled request says nothing about any channel.
	if errors.Is(err, context.Canceled) {
		return relayAttemptFatal
	}
	// Channel-local failures carry a "channel:" error code (egress/transport
	// trouble, no usable upstream status): mono's FailureClass::Local shape —
	// the candidate dies, switch to another.
	if types.IsChannelError(err) {
		return relayAttemptRetryable
	}
	switch {
	case err.StatusCode == http.StatusTooManyRequests:
		return relayAttemptThrottled
	case err.StatusCode == http.StatusUnauthorized,
		err.StatusCode == http.StatusForbidden,
		err.StatusCode == http.StatusNotFound:
		// Channel-scoped 4xx: this channel/key/model combination is
		// rejected upstream, which says nothing about its siblings.
		return relayAttemptFatalSwitchable
	case err.StatusCode == http.StatusRequestTimeout || err.StatusCode >= http.StatusInternalServerError:
		return relayAttemptRetryable
	case err.StatusCode >= http.StatusBadRequest:
		// Caller-side 4xx (400/413/422, ...): the request itself is
		// unserveable, every channel would reject it the same way.
		return relayAttemptFatal
	case err.StatusCode == 0 || err.StatusCode < http.StatusOK || err.StatusCode < http.StatusBadRequest:
		// No status (transport-level error) or 1xx/3xx: the range policy
		// retries them today, so a candidate switch stays on the table.
		return relayAttemptRetryable
	}
	// A 2xx response carrying an error body: today the range policy does not
	// retry it, so pass it through without a switch.
	return relayAttemptFatal
}

// relayFailoverExhausted is the failover loop's terminal error: 503 when no
// route unit is cooling, 504 + Retry-After when the pool recovers from a
// cooldown. The TYPE (not a field on NewAPIError) carries the header
// decision, so a stashed switchable-4xx that passes through stays the
// upstream's error unmodified — the terminal decision is surfaced at the
// loop's existing error-writing site, which checks for this type.
type relayFailoverExhausted struct {
	// RetryAfterSec > 0 → a `Retry-After` header on the wire response.
	RetryAfterSec int
}

func (e relayFailoverExhausted) Error() string {
	if e.RetryAfterSec > 0 {
		return fmt.Sprintf("所有路由正在冷却恢复中，请在 %d 秒后重试", e.RetryAfterSec)
	}
	return "所有路由暂时不可用"
}

// newFailoverTerminalError is mono's terminal decision: a stashed
// channel-scoped 4xx passes through to the client verbatim; otherwise the
// terminal error is 504 + Retry-After while any unit is cooling (the client
// re-sends after recovery) and 503 with no wait when nothing is cooling
// (nothing can get better — the client may retry immediately).
func newFailoverTerminalError(stashed *types.NewAPIError, coolingMs int64) *types.NewAPIError {
	if stashed != nil {
		return stashed
	}
	if coolingMs > 0 {
		seconds := int((coolingMs + 999) / 1000) // round up: the client waits at least until recovery
		return types.NewErrorWithStatusCode(relayFailoverExhausted{RetryAfterSec: seconds}, types.ErrorCodeGetChannelFailed, http.StatusGatewayTimeout)
	}
	return types.NewErrorWithStatusCode(relayFailoverExhausted{}, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
}

// newConcurrencySlotFullError is the loop's local 429 for a full
// concurrency slot pool (mono's rate_limited degraded arm): retryable by
// the client, channel-UNscoped — the code is not a channel:* one, so the
// health table, the channel-outcome ledger and the autoban path never
// see it as an upstream failure.
func newConcurrencySlotFullError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("concurrency limit reached"),
		types.ErrorCodeRateLimited,
		http.StatusTooManyRequests,
	)
}

// terminalFailoverError is the loop's terminal-error decision at pool
// exhaustion: when the pool saturated, the last slot-full pass's local
// 429 is the authority (nothing failed upstream — a 503/504 wrap would
// blame the pool for our own gate); otherwise the stashed switchable-4xx
// / 504+Retry-After / 503 split of newFailoverTerminalError applies.
func terminalFailoverError(slotFull *types.NewAPIError, stashed *types.NewAPIError, coolingMs int64) *types.NewAPIError {
	if slotFull != nil {
		return slotFull
	}
	return newFailoverTerminalError(stashed, coolingMs)
}

// coolDownSleep is the failover loop's select-aware cooldown-recovery wait:
// time the request context can cancel, sleep until the pool's shortest
// cooldown deadline. It reports whether the sleep was cut short by the
// cancellation.
func coolDownSleep(c contract.Context, d time.Duration) bool {
	if d <= 0 {
		return false
	}
	ctx := c.Context()
	if ctx == nil {
		time.Sleep(d)
		return false
	}
	select {
	case <-time.After(d):
		return false
	case <-ctx.Done():
		return true
	}
}

// failoverPolicy builds the request-level failover budget for the relay loop
// from the live option config: the attempt cap rides the existing RetryTimes
// option (mono's max_attempts counts the first dial; RetryTimes counts the
// retries after it), the time budget is the hot-swapped option — failed
// attempt latencies and cooldown waits only, a success charges nothing.
func failoverPolicy() catalog.FailoverPolicy {
	return catalog.FailoverPolicy{
		MaxAttempts:   common.RetryTimes + 1,
		TotalBudgetMs: catalog.GetGatewayRetryTotalBudgetMs(),
	}
}
