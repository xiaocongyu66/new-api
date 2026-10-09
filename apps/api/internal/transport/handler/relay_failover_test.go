package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// TestClassifyAttemptIsMonoFailureClasses is the port gate for the failover
// loop's failure classification: it pins the shape mono's egress status
// classification gives each status, layered under the admin's range policy
// (which the loop ANDs in separately — classifyAttempt says "what happened",
// it never says "may we retry").
func TestClassifyAttemptIsMonoFailureClasses(t *testing.T) {
	cases := []struct {
		name string
		err  *types.NewAPIError
		want relayAttemptKind
	}{
		{"nil is the success arm", nil, relayAttemptDone},
		{"canceled request is fatal",
			types.NewError(context.Canceled, types.ErrorCodeDoRequestFailed, types.ErrOptionWithStatusCode(http.StatusInternalServerError)),
			relayAttemptFatal},
		{"channel-scoped transport error switches",
			types.NewError(errors.New("dial: connection refused"), types.ErrorCodeChannelInvalidKey, types.ErrOptionWithStatusCode(http.StatusInternalServerError)),
			relayAttemptRetryable},
		{"429 is throttled and carries the hint",
			types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests,
				types.ErrOptionWithRetryAfterMs(15_000)),
			relayAttemptThrottled},
		{"channel-scoped 401 is switchable",
			types.NewOpenAIError(errors.New("bad key"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized),
			relayAttemptFatalSwitchable},
		{"403 is switchable",
			types.NewOpenAIError(errors.New("forbidden"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden),
			relayAttemptFatalSwitchable},
		{"404 is switchable",
			types.NewOpenAIError(errors.New("model not found"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound),
			relayAttemptFatalSwitchable},
		{"caller-side 400 is fatal",
			types.NewOpenAIError(errors.New("bad request body"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest),
			relayAttemptFatal},
		{"caller-side 413 is fatal",
			types.NewOpenAIError(errors.New("body too large"), types.ErrorCodeBadResponseStatusCode, http.StatusRequestEntityTooLarge),
			relayAttemptFatal},
		{"caller-side 422 is fatal",
			types.NewOpenAIError(errors.New("unprocessable"), types.ErrorCodeBadResponseStatusCode, http.StatusUnprocessableEntity),
			relayAttemptFatal},
		{"408 is retryable",
			types.NewOpenAIError(errors.New("upstream timed out"), types.ErrorCodeBadResponseStatusCode, http.StatusRequestTimeout),
			relayAttemptRetryable},
		{"500 is retryable",
			types.NewOpenAIError(errors.New("upstream exploded"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
			relayAttemptRetryable},
		{"503 is retryable",
			types.NewOpenAIError(errors.New("upstream down"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable),
			relayAttemptRetryable},
		{"no status (transport-level error) is retryable",
			types.NewError(errors.New("dial failed"), types.ErrorCodeDoRequestFailed),
			relayAttemptRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAttempt(tc.err); got != tc.want {
				t.Fatalf("classifyAttempt() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewFailoverTerminalErrorSplit pins mono's terminal decision: a stashed
// switchable-4xx passes through unmodified; otherwise 504 + Retry-After
// (rounded up to whole seconds) while anything is cooling, plain 503 when
// nothing is.
func TestNewFailoverTerminalErrorSplit(t *testing.T) {
	stashed := types.NewOpenAIError(errors.New("bad key"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	if got := newFailoverTerminalError(stashed, 0); got != stashed {
		t.Fatalf("stashed error must pass through unmodified, got %v", got)
	}
	if got := newFailoverTerminalError(nil, 0); got.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("cooling=0 terminal must be 503, got %d", got.StatusCode)
	}
	if got := newFailoverTerminalError(nil, 5_999); got.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("cooling>0 terminal must be 504, got %d", got.StatusCode)
	}
	var exhausted relayFailoverExhausted
	if got := newFailoverTerminalError(nil, 5_999); !errors.As(got.Err, &exhausted) || exhausted.RetryAfterSec != 6 {
		t.Fatalf("504 must carry Retry-After rounded up to whole seconds, got %d", exhausted.RetryAfterSec)
	}
	if got := newFailoverTerminalError(nil, 1_000); !errors.As(got.Err, &exhausted) || exhausted.RetryAfterSec != 1 {
		t.Fatalf("504 with an exact second boundary must carry Retry-After: 1, got %d", exhausted.RetryAfterSec)
	}
}

// TestNewConcurrencySlotFullError pins the loop's local 429 shape:
// client-retryable, and NOT a channel:* code — the health table, the
// channel-outcome ledger and the autoban path must never treat our own
// gate's rejection as an upstream failure.
func TestNewConcurrencySlotFullError(t *testing.T) {
	err := newConcurrencySlotFullError()
	if err.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("slot-full error must be 429, got %d", err.StatusCode)
	}
	if types.IsChannelError(err) {
		t.Fatalf("the slot-full error must not be classified as a channel error")
	}
	if kind := classifyAttempt(err); kind != relayAttemptThrottled {
		// A 429 we produced ourselves still classifies as throttled, so
		// the loop's switchable-kind branch keeps a slot-full request
		// from charging the route unit's health machine.
		t.Fatalf("the slot-full 429 must classify as throttled, got %v", kind)
	}
}

// TestTerminalFailoverErrorPrecedence pins the pool-exhausted terminal
// decision: a saturated pool's local 429 is the terminal authority even
// over a stashed switchable-4xx or a live cooldown; only when no pass
// hit a full slot does the stashed / cooling split apply.
func TestTerminalFailoverErrorPrecedence(t *testing.T) {
	slotFull := newConcurrencySlotFullError()
	stashed := types.NewOpenAIError(errors.New("bad key"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)

	if got := terminalFailoverError(slotFull, stashed, 12_000); got != slotFull {
		t.Fatalf("a saturated pool must end the request with the local 429, not the stashed 401 or a 504")
	}
	if got := terminalFailoverError(slotFull, stashed, 0); got != slotFull {
		t.Fatalf("a saturated pool must end the request with the local 429, not a 503")
	}
	if got := terminalFailoverError(nil, stashed, 0); got != stashed {
		t.Fatalf("without a slot-full pass the stashed 4xx must pass through")
	}
	if got := terminalFailoverError(nil, nil, 4_500); got.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("cooling exhaustion must be a 504, got %d", got.StatusCode)
	}
	if got := terminalFailoverError(nil, nil, 0); got.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-cooling exhaustion must be a 503, got %d", got.StatusCode)
	}
}
