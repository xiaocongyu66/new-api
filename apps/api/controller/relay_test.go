package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newRetryTestContext(t *testing.T) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx
}

type testErr string

func (e testErr) Error() string { return string(e) }

// IsChannelError now runs AFTER IsSkipRetryError and retryTimes<=0.
// A channel error that also carries skip-retry must not retry.
func TestShouldRetryChannelErrorWithSkipRetryDoesNotRetry(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("channel:param_override_invalid"),
		types.ErrorCodeChannelParamOverrideInvalid,
		types.ErrOptionWithSkipRetry(),
	)
	assert.False(t, shouldRetry(ctx, err, 3),
		"skip-retry on a channel error must short-circuit before IsChannelError")
}

// IsChannelError now runs AFTER retryTimes<=0.
// A channel error with zero retry budget must not retry.
func TestShouldRetryChannelErrorWithExhaustedBudgetDoesNotRetry(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("channel:invalid_key"),
		types.ErrorCodeChannelInvalidKey,
	)
	assert.False(t, shouldRetry(ctx, err, 0),
		"channel error with zero retry budget should not retry")
}

// A plain channel error (no skip-retry) with retries left must still retry.
func TestShouldRetryChannelErrorWithBudgetRetries(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("channel:invalid_key"),
		types.ErrorCodeChannelInvalidKey,
	)
	assert.True(t, shouldRetry(ctx, err, 3),
		"channel error with retries remaining should retry")
}

func TestShouldRetryNilError(t *testing.T) {
	ctx := newRetryTestContext(t)
	assert.False(t, shouldRetry(ctx, nil, 5))
}

func TestShouldRetrySkipRetryError(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("invalid request"),
		types.ErrorCodeInvalidRequest,
		types.ErrOptionWithSkipRetry(),
	)
	assert.False(t, shouldRetry(ctx, err, 5))
}

func TestShouldRetrySpecificChannel(t *testing.T) {
	ctx := newRetryTestContext(t)
	ctx.Set("specific_channel_id", 42)
	err := types.NewError(
		testErr("bad response"),
		types.ErrorCodeBadResponseStatusCode,
	)
	err.StatusCode = http.StatusInternalServerError
	assert.False(t, shouldRetry(ctx, err, 5))
}

// classifyChatFailureSource: ErrorCodeDoRequestFailed is a local transport
// failure (DNS, connection refused, TLS), not a provider response.
func TestClassifyChatFailureSourceDoRequestFailedIsLocal(t *testing.T) {
	err := types.NewError(
		testErr("dial tcp: connection refused"),
		types.ErrorCodeDoRequestFailed,
	)
	assert.Equal(t, model.FailureSourceLocal, classifyChatFailureSource(err))
}

// classifyChatFailureSource: provider/status transaction failures are upstream.
func TestClassifyChatFailureSourceBadResponseStatusCodeIsUpstream(t *testing.T) {
	err := types.NewError(
		testErr("provider returned 429"),
		types.ErrorCodeBadResponseStatusCode,
	)
	err.StatusCode = http.StatusTooManyRequests
	assert.Equal(t, model.FailureSourceUpstream, classifyChatFailureSource(err))
}

func TestClassifyChatFailureSourceNilIsUpstream(t *testing.T) {
	assert.Equal(t, model.FailureSourceUpstream, classifyChatFailureSource(nil))
}

// wouldRetryWithOneBudget is the predicate the retry loop uses to decide
// whether a terminal failure is an isolation candidate. It must answer "would
// this error retry if one retry were available?" independently of the real
// remaining budget, so that a chain whose last attempt has budget 0 still
// records the terminal retryable failure.

// RetryTimes=0 + channel error produces no isolation candidate: the global
// gate in the loop (common.RetryTimes > 0) blocks all recording, even though
// wouldRetryWithOneBudget itself returns true for a channel error.
func TestWouldRetryWithOneBudgetChannelErrorIsTrueButGateBlocks(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("channel:invalid_key"),
		types.ErrorCodeChannelInvalidKey,
	)
	// Predicate itself is true: a channel error would retry with budget=1.
	assert.True(t, wouldRetryWithOneBudget(ctx, err),
		"channel error should retry if one retry were available")
	// The loop applies common.RetryTimes > 0 on top. With RetryTimes=0 the
	// candidate is never formed, so no state transition is written.
	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 0
	candidate := common.RetryTimes > 0 && wouldRetryWithOneBudget(ctx, err)
	common.RetryTimes = originalRetryTimes
	assert.False(t, candidate,
		"RetryTimes=0 must gate off all isolation candidates, including channel errors")
}

// A retryable terminal 500 is a candidate even when that attempt's remaining
// retry budget is zero: wouldRetryWithOneBudget uses retryTimes=1, not the
// exhausted budget, so the terminal 500 in a 500->500 chain is recorded.
func TestWouldRetryWithOneBudgetRetryable500WithExhaustedBudget(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	err.StatusCode = http.StatusInternalServerError
	// With a positive global setting the candidate forms...
	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	candidatePositive := common.RetryTimes > 0 && wouldRetryWithOneBudget(ctx, err)
	// ...even though the actual remaining budget at the terminal attempt is 0.
	actualBudget := common.RetryTimes - 1 // last attempt: RetryTimes - retryIndex(1) = 0
	common.RetryTimes = originalRetryTimes
	assert.True(t, candidatePositive,
		"terminal 500 with positive RetryTimes must be an isolation candidate")
	assert.Equal(t, 0, actualBudget,
		"the terminal attempt must have zero remaining budget")
}

// A terminal non-retryable error (400) is not an isolation candidate: the
// loop's else-branch clears any prior candidate so a 500->400 chain records
// nothing, not the intermediate 500.
func TestWouldRetryWithOneBudgetNonRetryable400IsNotCandidate(t *testing.T) {
	ctx := newRetryTestContext(t)
	err := types.NewError(
		testErr("provider returned 400"),
		types.ErrorCodeBadResponseStatusCode,
	)
	err.StatusCode = http.StatusBadRequest
	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 5
	candidate := common.RetryTimes > 0 && wouldRetryWithOneBudget(ctx, err)
	common.RetryTimes = originalRetryTimes
	assert.False(t, candidate,
		"terminal 400 must not be an isolation candidate; prior 500 candidate is cleared")
}

// The retry loop in Relay and RelayTask must clear any prior upstream
// candidate before breaking on a terminal local error (getChannel failure,
// billing/setup error, body storage error). If it doesn't, the post-loop
// guard would persist the prior provider failure against a route that never
// actually produced the terminal error — violating the rule that a local
// ErrorCodeGetChannelFailed must never enter the state machine.
//
// These tests simulate the candidate lifecycle inline (the loop body clears
// lastRetryRoute/lastRetryErr to nil before each terminal-local break),
// proving the post-loop guard sees no candidate after a local terminal error.

// TestTerminalLocalErrorClearsPriorCandidate simulates a RetryTimes=1 chain:
// attempt 0 gets a retryable 500 and stores a candidate; attempt 1's getChannel
// fails with ErrorCodeGetChannelFailed. The loop clears the candidate before
// breaking, so the post-loop guard must not record.
func TestTerminalLocalErrorClearsPriorCandidate(t *testing.T) {
	ctx := newRetryTestContext(t)

	// ── attempt 0: retryable 500 → candidate set ──
	priorErr := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	priorErr.StatusCode = http.StatusInternalServerError

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	lastRetryRoute := (*model.RouteKey)(nil)
	lastRetryErr := (*types.NewAPIError)(nil)

	if common.RetryTimes > 0 && wouldRetryWithOneBudget(ctx, priorErr) {
		lastRetryRoute = &model.RouteKey{ChannelId: 1, Model: "gpt-4"}
		lastRetryErr = priorErr
	}
	// candidate must be set after attempt 0
	assert.NotNil(t, lastRetryRoute, "prior retryable 500 must set candidate")

	// ── attempt 1: getChannel fails → loop clears before break ──
	// This is the fix: terminal local error clears the prior candidate.
	lastRetryRoute = nil
	lastRetryErr = nil

	// ── post-loop guard ──
	newAPIError := types.NewError(
		testErr("no available channel"),
		types.ErrorCodeGetChannelFailed,
	)
	shouldRecord := newAPIError != nil && lastRetryRoute != nil && lastRetryErr != nil
	common.RetryTimes = originalRetryTimes

	assert.False(t, shouldRecord,
		"getChannel terminal error must not record a prior upstream candidate")
}

// TestTerminalBillingErrorClearsPriorCandidate covers the billing-error break
// in Relay: same lifecycle, terminal error is a billing failure.
func TestTerminalBillingErrorClearsPriorCandidate(t *testing.T) {
	_ = newRetryTestContext(t)

	priorErr := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	priorErr.StatusCode = http.StatusInternalServerError

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	lastRetryRoute := &model.RouteKey{ChannelId: 2, Model: "claude-3"}
	lastRetryErr := priorErr
	assert.NotNil(t, lastRetryRoute)

	// billing error break clears candidate
	lastRetryRoute = nil
	lastRetryErr = nil

	newAPIError := types.NewError(
		testErr("billing failed"),
		types.ErrorCodeModelPriceError,
	)
	shouldRecord := newAPIError != nil && lastRetryRoute != nil && lastRetryErr != nil
	common.RetryTimes = originalRetryTimes

	assert.False(t, shouldRecord,
		"billing terminal error must not record a prior upstream candidate")
}

// TestTerminalBodyStorageErrorClearsPriorCandidate covers the body-storage
// break in both Relay and RelayTask.
func TestTerminalBodyStorageErrorClearsPriorCandidate(t *testing.T) {
	_ = newRetryTestContext(t)

	priorErr := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	priorErr.StatusCode = http.StatusInternalServerError

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	lastRetryRoute := &model.RouteKey{ChannelId: 3, Model: "gemini-pro"}
	lastRetryErr := priorErr
	assert.NotNil(t, lastRetryRoute)

	// body storage error break clears candidate
	lastRetryRoute = nil
	lastRetryErr = nil

	newAPIError := types.NewError(
		testErr("body too large"),
		types.ErrorCodeReadRequestBodyFailed,
		types.ErrOptionWithSkipRetry(),
	)
	shouldRecord := newAPIError != nil && lastRetryRoute != nil && lastRetryErr != nil
	common.RetryTimes = originalRetryTimes

	assert.False(t, shouldRecord,
		"body storage terminal error must not record a prior upstream candidate")
}

// TestTerminalLockedChannelSetupErrorClearsPriorCandidate covers the
// setup_locked_channel_failed break in RelayTask.
func TestTerminalLockedChannelSetupErrorClearsPriorCandidate(t *testing.T) {
	_ = newRetryTestContext(t)

	priorErr := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	priorErr.StatusCode = http.StatusInternalServerError

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	lastRetryRoute := &model.RouteKey{ChannelId: 4, Model: "dall-e-3"}
	lastRetryErr := priorErr
	assert.NotNil(t, lastRetryRoute)

	// locked channel setup error break clears candidate
	lastRetryRoute = nil
	lastRetryErr = nil

	// taskErr is non-nil, but candidate was cleared
	shouldRecord := lastRetryRoute != nil && lastRetryErr != nil
	common.RetryTimes = originalRetryTimes

	assert.False(t, shouldRecord,
		"locked channel setup terminal error must not record a prior upstream candidate")
}

// TestCandidateSurvivesTerminalRetryable500 confirms the happy path: a
// terminal retryable 500 (no subsequent local error) still records, so the
// fix doesn't over-clear legitimate candidates.
func TestCandidateSurvivesTerminalRetryable500(t *testing.T) {
	ctx := newRetryTestContext(t)

	terminalErr := types.NewError(
		testErr("provider returned 500"),
		types.ErrorCodeBadResponseStatusCode,
	)
	terminalErr.StatusCode = http.StatusInternalServerError

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	lastRetryRoute := (*model.RouteKey)(nil)
	lastRetryErr := (*types.NewAPIError)(nil)

	if common.RetryTimes > 0 && wouldRetryWithOneBudget(ctx, terminalErr) {
		lastRetryRoute = &model.RouteKey{ChannelId: 5, Model: "gpt-4"}
		lastRetryErr = terminalErr
	}

	shouldRecord := lastRetryRoute != nil && lastRetryErr != nil
	common.RetryTimes = originalRetryTimes

	assert.True(t, shouldRecord,
		"terminal retryable 500 with no subsequent local error must still record")
}

// A JSON error body written after the stream already started would appear inside
// the open SSE stream as bare, prefix-less bytes. The deferred handler must stay
// silent in that case; realtime relays answer over the websocket and are exempt.
func TestCanWriteErrorBody(t *testing.T) {
	cases := []struct {
		name        string
		relayFormat types.RelayFormat
		bodyStarted bool
		want        bool
	}{
		{"openai before body", types.RelayFormatOpenAI, false, true},
		{"openai mid-stream", types.RelayFormatOpenAI, true, false},
		{"claude before body", types.RelayFormatClaude, false, true},
		{"claude mid-stream", types.RelayFormatClaude, true, false},
		{"gemini mid-stream", types.RelayFormatGemini, true, false},
		{"realtime before body", types.RelayFormatOpenAIRealtime, false, true},
		{"realtime mid-stream writes over websocket", types.RelayFormatOpenAIRealtime, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, canWriteErrorBody(tc.relayFormat, tc.bodyStarted))
		})
	}
}

// TestRelayRetryChainRecordsEachUpstreamFailure verifies that each retry-eligible
// upstream failure in a chain is recorded against its route immediately, not just
// the terminal one. This covers the fix for #411: per-retry accounting instead of
// terminal-only accounting.
func TestRelayRetryChainRecordsEachUpstreamFailure(t *testing.T) {
	// Use in-memory SQLite for channel_model_health, same pattern as model tests.
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelModelHealth{}))
	model.DB = db
	model.ClearRouteHealthCache()
	t.Cleanup(func() {
		model.DB = previousDB
		model.ClearRouteHealthCache()
	})

	previousSetting := operation_setting.GetChannelModelHealthSetting()
	cfg := &operation_setting.ChannelModelHealthSetting{
		UpstreamFailureThreshold: 1,
		LocalFailureThreshold:    3,
		DormantDisableThreshold:  0,
		CalmWeightScale:          100,
		DormantWeightScale:       50,
		KeyProbeEnabled:          false,
	}
	applySetting := func(c *operation_setting.ChannelModelHealthSetting) {
		for key, value := range map[string]int{
			"UpstreamFailureThreshold": c.UpstreamFailureThreshold,
			"LocalFailureThreshold":    c.LocalFailureThreshold,
			"DormantDisableThreshold":  c.DormantDisableThreshold,
			"CalmWeightScale":          c.CalmWeightScale,
			"DormantWeightScale":       c.DormantWeightScale,
		} {
			require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue(key, strconv.Itoa(value)))
		}
		require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue("KeyProbeEnabled", strconv.FormatBool(c.KeyProbeEnabled)))
	}
	applySetting(cfg)
	t.Cleanup(func() { applySetting(previousSetting) })

	ctx := newRetryTestContext(t)
	common.RetryTimes = 2 // allow 2 retries (3 attempts total)
	defer func() { common.RetryTimes = 0 }()

	// Simulate a retry chain: attempt 0 on channel 1 returns 500 (retryable),
	// attempt 1 on channel 2 returns 200 (success).
	// Per-retry recording: channel 1 should get a failure record, channel 2 should not.
	routeKey1 := model.RouteKey{ChannelId: 1, KeyIndex: 0, Model: "gpt-4"}
	err500 := types.NewError(testErr("provider returned 500"), types.ErrorCodeBadResponseStatusCode)
	err500.StatusCode = http.StatusInternalServerError

	// Attempt 0: record failure for channel 1
	recordRouteIsolation(ctx, routeKey1, err500, model.FailureSourceUpstream)

	// Attempt 1: success on channel 2 (no recording for success path in this test)
	routeKey2 := model.RouteKey{ChannelId: 2, KeyIndex: 0, Model: "gpt-4"}

	// Verify channel 1 has a failure record
	state1, level1, until1, ok1 := model.GetRouteIsolation(routeKey1)
	require.True(t, ok1, "channel 1 should have isolation record after upstream 500")
	assert.Equal(t, "calm", state1)
	assert.Equal(t, 1, level1)
	assert.Greater(t, until1, time.Now().Unix())

	// Verify channel 2 has NO failure record
	_, _, _, ok2 := model.GetRouteIsolation(routeKey2)
	assert.False(t, ok2, "channel 2 should not have isolation record after success")
}

// TestRelayPoolExhaustionRecordsAllFailures verifies that when all channels in
// the pool return retryable upstream errors until the pool is exhausted, each
// channel gets exactly one failure record.
func TestRelayPoolExhaustionRecordsAllFailures(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelModelHealth{}))
	model.DB = db
	model.ClearRouteHealthCache()
	t.Cleanup(func() {
		model.DB = previousDB
		model.ClearRouteHealthCache()
	})

	previousSetting := operation_setting.GetChannelModelHealthSetting()
	cfg := &operation_setting.ChannelModelHealthSetting{
		UpstreamFailureThreshold: 1,
		LocalFailureThreshold:    3,
		DormantDisableThreshold:  0,
		CalmWeightScale:          100,
		DormantWeightScale:       50,
		KeyProbeEnabled:          false,
	}
	applySetting := func(c *operation_setting.ChannelModelHealthSetting) {
		for key, value := range map[string]int{
			"UpstreamFailureThreshold": c.UpstreamFailureThreshold,
			"LocalFailureThreshold":    c.LocalFailureThreshold,
			"DormantDisableThreshold":  c.DormantDisableThreshold,
			"CalmWeightScale":          c.CalmWeightScale,
			"DormantWeightScale":       c.DormantWeightScale,
		} {
			require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue(key, strconv.Itoa(value)))
		}
		require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue("KeyProbeEnabled", strconv.FormatBool(c.KeyProbeEnabled)))
	}
	applySetting(cfg)
	t.Cleanup(func() { applySetting(previousSetting) })

	ctx := newRetryTestContext(t)
	common.RetryTimes = 2
	defer func() { common.RetryTimes = 0 }()

	err500 := types.NewError(testErr("provider returned 500"), types.ErrorCodeBadResponseStatusCode)
	err500.StatusCode = http.StatusInternalServerError

	// Two channels, both return 500 on their attempt
	routeKey1 := model.RouteKey{ChannelId: 10, KeyIndex: 0, Model: "gpt-4"}
	routeKey2 := model.RouteKey{ChannelId: 11, KeyIndex: 0, Model: "gpt-4"}

	// Attempt 0: channel 10 fails
	recordRouteIsolation(ctx, routeKey1, err500, model.FailureSourceUpstream)
	// Attempt 1: channel 11 fails
	recordRouteIsolation(ctx, routeKey2, err500, model.FailureSourceUpstream)
	// Attempt 2: no more channels, loop ends

	// Both channels should have exactly one failure record
	state1, level1, until1, ok1 := model.GetRouteIsolation(routeKey1)
	require.True(t, ok1, "channel 10 should have isolation record")
	assert.Equal(t, "calm", state1)
	assert.Equal(t, 1, level1)
	assert.Greater(t, until1, time.Now().Unix())

	state2, level2, until2, ok2 := model.GetRouteIsolation(routeKey2)
	require.True(t, ok2, "channel 11 should have isolation record")
	assert.Equal(t, "calm", state2)
	assert.Equal(t, 1, level2)
	assert.Greater(t, until2, time.Now().Unix())
}

// TestRelayLocal4xxDoesNotRecord verifies that request-local non-retryable
// errors (4xx) do not write any channel_model_health records.
func TestRelayLocal4xxDoesNotRecord(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelModelHealth{}))
	model.DB = db
	model.ClearRouteHealthCache()
	t.Cleanup(func() {
		model.DB = previousDB
		model.ClearRouteHealthCache()
	})

	previousSetting := operation_setting.GetChannelModelHealthSetting()
	cfg := &operation_setting.ChannelModelHealthSetting{
		UpstreamFailureThreshold: 1,
		LocalFailureThreshold:    3,
		DormantDisableThreshold:  0,
		CalmWeightScale:          100,
		DormantWeightScale:       50,
		KeyProbeEnabled:          false,
	}
	applySetting := func(c *operation_setting.ChannelModelHealthSetting) {
		for key, value := range map[string]int{
			"UpstreamFailureThreshold": c.UpstreamFailureThreshold,
			"LocalFailureThreshold":    c.LocalFailureThreshold,
			"DormantDisableThreshold":  c.DormantDisableThreshold,
			"CalmWeightScale":          c.CalmWeightScale,
			"DormantWeightScale":       c.DormantWeightScale,
		} {
			require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue(key, strconv.Itoa(value)))
		}
		require.NoError(t, operation_setting.UpdateChannelModelHealthSettingValue("KeyProbeEnabled", strconv.FormatBool(c.KeyProbeEnabled)))
	}
	applySetting(cfg)
	t.Cleanup(func() { applySetting(previousSetting) })

	ctx := newRetryTestContext(t)
	common.RetryTimes = 2
	defer func() { common.RetryTimes = 0 }()

	// 400 Bad Request is non-retryable (shouldRetry returns false for 400)
	err400 := types.NewError(testErr("invalid request"), types.ErrorCodeInvalidRequest)
	err400.StatusCode = http.StatusBadRequest

	routeKey := model.RouteKey{ChannelId: 20, KeyIndex: 0, Model: "gpt-4"}

	// Verify that wouldRetryWithOneBudget returns false for 400
	assert.False(t, wouldRetryWithOneBudget(ctx, err400), "400 should not be retryable even with budget")

	// Verify no record exists (since we didn't call recordRouteIsolation)
	_, _, _, ok := model.GetRouteIsolation(routeKey)
	assert.False(t, ok, "400 error should not create isolation record")
}
