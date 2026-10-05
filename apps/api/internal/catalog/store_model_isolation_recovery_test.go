package channel

// Recovery symmetry for model-level isolation. DisableChannelModel retires a
// model by flipping abilities.enabled and channel_model_routes.enabled to
// false; every path that undoes a health verdict must put them back. A 429 is
// a busy upstream rather than a dead model, and a hard disable must lapse on
// its own instead of stranding the route until an operator edits the channel.
//
// The invariants under test: throttling never escalates to a per-model
// disable, an auto-disable carries an expiry that restores full weight, and the
// admin recover action returns the model to the routable set — not just to a
// healthy health row.

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withCooldownTestClock(t *testing.T, now *time.Time) {
	t.Helper()

	previous := ChannelHealthNow
	ChannelHealthNow = func() time.Time { return *now }
	t.Cleanup(func() { ChannelHealthNow = previous })
}

// escalateToDisabled walks the ladder until the auto-disable trips, then
// returns the instant the route entered the disabled state.
func escalateToDisabled(t *testing.T, key RouteKey, cfg *ChannelModelHealthSetting, start time.Time) time.Time {
	t.Helper()

	now := start
	for range 200 {
		if _, disabled := cachedRouteState(key); disabled {
			return now
		}
		require.NoError(t, RecordRetryableFailure(key, "bad_response", FailureSourceUpstream, now))
		now = now.Add(time.Duration(cfg.DormantMaxBase) * time.Second)
	}
	t.Fatal("the auto-disable never tripped")
	return now
}

// cachedRouteState reads the in-process snapshot the selection path consults.
func cachedRouteState(key RouteKey) (string, bool) {
	routeHealthLock.RLock()
	defer routeHealthLock.RUnlock()
	state := routeHealthIDM[key]
	if state == nil {
		return HealthHealthy, false
	}
	return state.State, state.State == HealthDisabled
}

// TestThrottlingNeverDisablesTheModel pins the production incident: a stream of
// 429s drove CooldownDisableStreak to its limit and permanently retired working
// models, leaving 37 of 41 models unschedulable. A throttled upstream must only
// back off.
func TestThrottlingNeverDisablesTheModel(t *testing.T) {
	previousStore := ChannelModelDisabler
	var mu sync.Mutex
	var disabledFor []string
	disablerDone := make(chan struct{})
	ChannelModelDisabler = func(channelID int, modelName string) error {
		mu.Lock()
		disabledFor = append(disabledFor, modelName)
		mu.Unlock()
		close(disablerDone)
		return nil
	}
	t.Cleanup(func() { ChannelModelDisabler = previousStore })

	store := &HealthStore{states: map[int]*ChannelHealthState{}}
	cfg := DefaultChannelHealthSetting()
	cfg.MinRequests = 0
	// One cooldown is enough to reach the escalation threshold, so any single
	// throttled attempt that escalates fails this test.
	cfg.CooldownThreshold = 1
	cfg.CooldownDisableStreak = 1
	previousSetting := GetChannelHealthSetting()
	channelHealthSetting.Store(cfg)
	t.Cleanup(func() { channelHealthSetting.Store(previousSetting) })

	now := time.Unix(1_700_000_000, 0)
	withCooldownTestClock(t, &now)
	for range 20 {
		store.recordChannelOutcome(1, "throttled-model", OutcomeThrottled)
	}
	mu.Lock()
	afterThrottle := append([]string(nil), disabledFor...)
	mu.Unlock()
	assert.Empty(t, afterThrottle, "429s must never disable a model")

	// The escalation path itself must still work for a genuine failure, or the
	// guard above would pass on a broken escalation. escalateModelLocked runs the
	// disable in a goroutine, so wait for it instead of racing the assertion.
	store.recordChannelOutcome(1, "failed-model", OutcomeFatal)
	select {
	case <-disablerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the fatal outcome never reached the per-model disable")
	}
	assert.Equal(t, []string{"failed-model"}, disabledFor,
		"a real failure must still reach the per-model disable")
}

// TestAutoDisabledRouteRecoversAfterItsWindow covers the dead end: the disable
// wrote until=nil, which no expiry path could retire and which emergency
// recovery explicitly skipped, so one outage removed a model forever. The
// selection path reads the in-process snapshot, so the window has to lapse
// there too — not only in IsRouteHealthy.
func TestAutoDisabledRouteRecoversAfterItsWindow(t *testing.T) {
	withRouteHealthDB(t)
	cfg := DefaultChannelModelHealthSetting()
	// The auto-disable fires an asynchronous key probe; the probe is nil in
	// tests, so it returns without touching state. Disable the branch anyway so
	// the test states its own precondition instead of depending on that.
	cfg.KeyProbeEnabled = false
	withHealthSetting(t, cfg)
	previousPressure := pressureIDM
	// A pool of one unit is a small pool, which skips the auto-disable; force
	// the large-pool branch so the threshold path is the one under test.
	pressureIDM = map[string]*modelPressure{"recovers": {total: 8, healthy: 8}}
	t.Cleanup(func() { pressureIDM = previousPressure })

	// RecordRetryableFailure stamps `until` from the `now` it is handed, while
	// the selection path reads the package clock. Start from the real clock so
	// the two timelines agree, then drive the clock forward to cross the window.
	now := ChannelHealthNow()
	withCooldownTestClock(t, &now)

	key := RouteKey{ChannelId: 9301, Model: "recovers"}
	disabledAt := escalateToDisabled(t, key, cfg, now)

	assert.False(t, IsRouteSelectable(key), "a disabled route is excluded inside its window")
	assert.Equal(t, 0.0, RouteWeightMultiplier(key), "a disabled route weighs nothing inside its window")

	// Cross the window the auto-disable wrote and the route must come back on
	// its own — no admin action, no restart.
	now = disabledAt.Add(time.Duration(cfg.DormantMaxBase)*time.Second + time.Second)
	assert.True(t, IsRouteSelectable(key),
		"the disable must lapse on its own once the window elapses")
	assert.Equal(t, 1.0, RouteWeightMultiplier(key),
		"a recovered route returns to full weight, not a reduced share")

	var row ChannelModelHealth
	require.NoError(t, dbx.DB.Where("channel_id = ? AND model = ?", key.ChannelId, key.Model).First(&row).Error)
	assert.Equal(t, HealthHealthy, row.State, "the expiry CAS must persist the recovery")
	_, stillDisabled := cachedRouteState(key)
	assert.False(t, stillDisabled, "the in-process snapshot must agree with the persisted recovery")
}

// TestAdminRecoverRestoresTheRoutableModel is the operator-facing half of the
// same asymmetry: recovering the health row alone left abilities and route rows
// disabled, so the action reported success while the model stayed invisible to
// every group.
func TestAdminRecoverRestoresTheRoutableModel(t *testing.T) {
	setupIsolationTest(t)
	seedIsolationChannel(t, 9302, "model-a,model-b")
	require.NoError(t, DisableChannelModel(9302, "model-a"))
	require.Equal(t, map[string]bool{"model-a": false, "model-b": true}, abilityEnabledByModel(t, 9302))

	key := RouteKey{ChannelId: 9302, Model: "model-a"}
	require.NoError(t, RecoverRoute(key, time.Unix(1_700_000_000, 0)))

	assert.Equal(t, map[string]bool{"model-a": true, "model-b": true}, abilityEnabledByModel(t, 9302),
		"recovering a model must return its ability rows to the routable set")
	assert.Equal(t, map[string]bool{"model-a": true, "model-b": true}, routeEnabledByAlias(t, 9302),
		"route rows must follow the restored ability, or the model still gets no traffic")
}

// TestModelCooldownCountDecaysOnSuccess pins the "down" direction of the
// escalation counter. Only the disable itself used to clear the entry, so the
// count was a lifetime total: cooldowns spread over months retired a model that
// had served thousands of requests since. A served request must pay it back
// down, which is what makes the escalation mean sustained recent trouble.
func TestModelCooldownCountDecaysOnSuccess(t *testing.T) {
	previousStore := ChannelModelDisabler
	var mu sync.Mutex
	var disabledFor []string
	fired := make(chan struct{}, 8)
	ChannelModelDisabler = func(channelID int, modelName string) error {
		mu.Lock()
		disabledFor = append(disabledFor, modelName)
		mu.Unlock()
		fired <- struct{}{}
		return nil
	}
	t.Cleanup(func() { ChannelModelDisabler = previousStore })

	store := &HealthStore{states: map[int]*ChannelHealthState{}}
	cfg := DefaultChannelHealthSetting()
	cfg.MinRequests = 0
	// One cooldown per fatal outcome, so the test drives the counter directly
	// instead of having to build a failure streak first.
	cfg.CooldownThreshold = 1
	cfg.CooldownDisableStreak = 3
	previousSetting := GetChannelHealthSetting()
	channelHealthSetting.Store(cfg)
	t.Cleanup(func() { channelHealthSetting.Store(previousSetting) })

	now := time.Unix(1_700_000_000, 0)
	withCooldownTestClock(t, &now)

	count := func() int {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.states[1].ModelCooldowns["flaky"]
	}
	disabled := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), disabledFor...)
	}

	store.recordChannelOutcome(1, "flaky", OutcomeFatal)
	require.Equal(t, 1, count(), "a cooldown activation counts up")

	store.recordChannelOutcome(1, "flaky", OutcomeSuccess)
	assert.Zero(t, count(), "a served request must pay the escalation count back down")

	// Two activations now sit below the threshold, so nothing may fire.
	store.recordChannelOutcome(1, "flaky", OutcomeFatal)
	store.recordChannelOutcome(1, "flaky", OutcomeFatal)
	require.Equal(t, 2, count())
	select {
	case <-fired:
		t.Fatal("the model was retired while the count sat below the threshold")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Empty(t, disabled())

	// Positive control: reaching the threshold must still retire the model, so
	// the assertions above cannot pass on a broken escalation.
	store.recordChannelOutcome(1, "flaky", OutcomeFatal)
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the count reached the threshold but the disable never fired")
	}
	assert.Equal(t, []string{"flaky"}, disabled())
}
