package channel

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// HealthStore holds the channel health scoring state and implements the
// business logic for EWMA scoring, cooldowns, and routing weight calculation.
// It is the logical owner of channel health state; model.ChannelHealthManager
// is a thin forwarding facade that calls into this store via a registered bridge.
type HealthStore struct {
	mu     sync.Mutex
	states map[int]*model.ChannelHealthState
}

var (
	healthStoreOnce sync.Once
	healthStore     *HealthStore
)

// GetHealthStore returns the singleton health store.
func GetHealthStore() *HealthStore {
	healthStoreOnce.Do(func() {
		healthStore = &HealthStore{
			states: make(map[int]*model.ChannelHealthState),
		}
	})
	return healthStore
}

// ResetHealthStoreForTest swaps in a fresh store for capability-package tests.
func ResetHealthStoreForTest() {
	healthStoreOnce = sync.Once{}
}

// ClassifyChannelOutcome categorizes an API error into a ChannelOutcome.
// It is concurrency-safe and does NOT respect the kill switch; classification
// drives request-level exclusion which should always operate.
func (h *HealthStore) ClassifyChannelOutcome(err *types.NewAPIError, channelID int) model.ChannelOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, tracked := h.states[channelID]
	if !tracked {
		state = &model.ChannelHealthState{EwmaScore: model.DefaultScore}
	}

	outcome := classifyChannelOutcomeUnlocked(state, err)

	// A live 401 run is the only classification state worth carrying between
	// requests; everything else is derived fresh from the error each time.
	if !tracked && state.UnauthorizedRun > 0 {
		h.states[channelID] = state
	}

	return outcome
}

// classifyChannelOutcomeUnlocked classifies the error assuming the store mutex
// is already held by the caller.
func classifyChannelOutcomeUnlocked(state *model.ChannelHealthState, err *types.NewAPIError) model.ChannelOutcome {
	if err == nil {
		state.UnauthorizedRun = 0
		return model.OutcomeSuccess
	}

	// Channel errors or bad response body are always fatal.
	if types.IsChannelError(err) || err.GetErrorCode() == types.ErrorCodeBadResponseBody {
		state.UnauthorizedRun = 0
		return model.OutcomeFatal
	}

	// 429 => throttled, reset unauthorized run.
	if err.StatusCode == 429 {
		state.UnauthorizedRun = 0
		return model.OutcomeThrottled
	}

	// 5xx => fatal.
	if err.StatusCode >= 500 {
		state.UnauthorizedRun = 0
		return model.OutcomeFatal
	}

	// 401 => count the run and escalate once it is sustained.
	if err.StatusCode == 401 {
		if state.UnauthorizedRun < model.UnauthorizedEscalationThreshold {
			state.UnauthorizedRun++
		}
		if state.UnauthorizedRun >= model.UnauthorizedEscalationThreshold {
			return model.OutcomeFatal
		}
		return model.OutcomeNeutral
	}

	// All other status codes => neutral, reset unauthorized run.
	state.UnauthorizedRun = 0
	return model.OutcomeNeutral
}

// RecordChannelOutcome updates the EWMA score for a channel based on a
// ChannelOutcome. The kill switch gates health scoring only.
func (h *HealthStore) RecordChannelOutcome(channelID int, outcome model.ChannelOutcome) {
	h.recordChannelOutcome(channelID, "", outcome)
}

// recordChannelOutcome is the model-aware form. An empty modelName records
// health exactly as before but cannot escalate to a per-model disable.
func (h *HealthStore) recordChannelOutcome(channelID int, modelName string, outcome model.ChannelOutcome) {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[channelID]
	if !ok {
		state = &model.ChannelHealthState{
			EwmaScore:       model.DefaultScore,
			RequestCount:    0,
			UnauthorizedRun: 0,
		}
		h.states[channelID] = state
	}

	now := model.ChannelHealthNow()

	// Finish any expired cooldown before applying the new outcome so a
	// post-expiry result re-enters the slow-start curve cleanly.
	if !state.CooldownUntil.IsZero() && !state.CooldownUntil.After(now) {
		finishCooldownLocked(state)
	}

	switch outcome {
	case model.OutcomeSuccess:
		state.FailureStreak = 0
		// A clean success once a cooldown has expired decays the streak so the
		// next failure run starts from a shorter duration.
		if state.CooldownStreak > 0 && state.CooldownUntil.IsZero() {
			state.CooldownStreak--
		}
	case model.OutcomeNeutral:
		// Neutral stays score/request-count inert but clears an accumulated
		// failure streak: it is not evidence against the channel.
		state.FailureStreak = 0
		return
	case model.OutcomeFatal, model.OutcomeThrottled:
		state.FailureStreak++
	}

	// Apply the appropriate observation via the shared EWMA update.
	var observation float64
	switch outcome {
	case model.OutcomeSuccess:
		observation = 1.0
	case model.OutcomeFatal:
		observation = 0.0
	case model.OutcomeThrottled:
		observation = model.ThrottledObservation
	default:
		observation = 0.0
	}

	// Increment request count and update EWMA.
	state.RequestCount++
	state.RampPending = false

	// A fatal outcome ends the warm-up ramp immediately.
	if outcome == model.OutcomeFatal {
		state.RampExited = true
	}

	if state.RequestCount > cfg.MinRequests {
		state.EwmaScore = cfg.Alpha*observation + (1-cfg.Alpha)*state.EwmaScore
		if state.EwmaScore < cfg.MinScore {
			state.EwmaScore = cfg.MinScore
		}
	}

	// The cooldown trigger is deliberately outside the MinRequests guard.
	if (outcome == model.OutcomeFatal || outcome == model.OutcomeThrottled) && state.FailureStreak >= cfg.CooldownThreshold {
		startCooldownLocked(state, cfg, now)
		if modelName != "" {
			h.escalateModelLocked(state, cfg, channelID, modelName)
		}
	}
}

// escalateModelLocked counts this cooldown against modelName and disables that
// model on the channel once the count reaches CooldownDisableStreak. Caller
// must hold h.mu.
func (h *HealthStore) escalateModelLocked(state *model.ChannelHealthState, cfg *operation_setting.ChannelHealthSetting, channelID int, modelName string) {
	if cfg.CooldownDisableStreak <= 0 {
		return
	}
	if state.ModelCooldowns == nil {
		state.ModelCooldowns = make(map[string]int, 1)
	}
	state.ModelCooldowns[modelName]++
	if state.ModelCooldowns[modelName] < cfg.CooldownDisableStreak {
		return
	}
	reached := state.ModelCooldowns[modelName]
	delete(state.ModelCooldowns, modelName)
	disable := model.ChannelModelDisabler
	go func() {
		if err := disable(channelID, modelName); err != nil {
			common.SysError(fmt.Sprintf("failed to disable model %q on channel %d after repeated cooldowns: %s",
				modelName, channelID, err.Error()))
			// Put the count back so the next cooldown retries the disable.
			h.mu.Lock()
			defer h.mu.Unlock()
			if current, ok := h.states[channelID]; ok {
				if current.ModelCooldowns == nil {
					current.ModelCooldowns = make(map[string]int, 1)
				}
				if current.ModelCooldowns[modelName] < reached {
					current.ModelCooldowns[modelName] = reached
				}
			}
			return
		}
		common.SysLog(fmt.Sprintf("model %q disabled on channel %d: cooldown repeated %d times without recovery",
			modelName, channelID, reached))
	}()
}

// RecordRequestAttempts applies health accounting once for a whole client
// request, rather than once per failed try.
func (h *HealthStore) RecordRequestAttempts(attempts []model.ChannelAttempt, winnerID int, succeeded bool) {
	if succeeded {
		// ClassifyChannelOutcome(nil, ...) also clears any in-flight 401 run,
		// which a bare OutcomeSuccess would leave standing.
		h.RecordChannelOutcome(winnerID, h.ClassifyChannelOutcome(nil, winnerID))
		return
	}
	for _, attempt := range attempts {
		if attempt.Outcome.AffectsHealth() {
			h.recordChannelOutcome(attempt.ChannelID, attempt.ModelName, attempt.Outcome)
		}
	}
}

// RecordOutcome updates the EWMA score for a channel after a request (legacy API).
// success=true means the request succeeded; false means it failed.
// The kill switch is checked: if disabled, the method returns immediately.
func (h *HealthStore) RecordOutcome(channelID int, success bool) {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[channelID]
	if !ok {
		state = &model.ChannelHealthState{
			EwmaScore:       model.DefaultScore,
			RequestCount:    0,
			UnauthorizedRun: 0,
		}
		h.states[channelID] = state
	}

	state.RequestCount++

	// A real failure ends the warm-up ramp immediately.
	if !success {
		state.RampExited = true
	}

	// Don't update EWMA until we have enough data; trust the channel.
	if state.RequestCount <= cfg.MinRequests {
		return
	}

	outcome := 0.0
	if success {
		outcome = 1.0
	}

	state.EwmaScore = cfg.Alpha*outcome + (1-cfg.Alpha)*state.EwmaScore
	if state.EwmaScore < cfg.MinScore {
		state.EwmaScore = cfg.MinScore
	}
}

// slowStartFactor scales a channel's routing weight during its warm-up window.
// Caller must hold h.mu.
func slowStartFactor(state *model.ChannelHealthState, minRequests int) float64 {
	if minRequests <= 0 || state.RampExited {
		return 1.0
	}
	if state.RampPending {
		return 1.0 / float64(minRequests)
	}
	if state.RequestCount == 0 || state.RequestCount >= minRequests {
		return 1.0
	}
	return float64(state.RequestCount) / float64(minRequests)
}

// EffectiveWeight returns the routing weight for a channel, scaled by its EWMA
// health score and slow-start factor. A channel in live cooldown weighs zero.
func (h *HealthStore) EffectiveWeight(channelID int, baseWeight uint) float64 {
	return h.RoutingWeight(channelID, baseWeight, false)
}

// RoutingWeight is the shared health/cooldown weight path. EffectiveWeight calls
// it with bypassCooldown=false. Selectors call it with true for cooling
// candidates deliberately retained by the max-ejection cap.
func (h *HealthStore) RoutingWeight(channelID int, baseWeight uint, bypassCooldown bool) float64 {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return float64(baseWeight)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[channelID]
	if !ok {
		return float64(baseWeight) // no history = full health, no ramp to apply
	}

	now := model.ChannelHealthNow()
	if !state.CooldownUntil.IsZero() {
		if state.CooldownUntil.After(now) {
			if !bypassCooldown {
				return 0
			}
		} else {
			// Expired: finish it lazily so the recovered channel re-enters the
			// slow-start curve on this very selection.
			finishCooldownLocked(state)
		}
	}

	return float64(baseWeight) * state.EwmaScore * slowStartFactor(state, cfg.MinRequests)
}

// CooldownDuration computes the sliding cooldown duration.
func CooldownDuration(cfg *operation_setting.ChannelHealthSetting, priorActivations int) time.Duration {
	base, max := cfg.CooldownBaseSeconds, cfg.CooldownMaxSeconds
	if base <= 0 || max <= 0 {
		return 0
	}
	if max < base {
		max = base
	}
	// alpha^0 == 1, so the first activation is exactly base.
	factor := 1.0 - math.Pow(cfg.CooldownAlpha, float64(priorActivations))
	d := float64(base) + float64(max-base)*factor
	if d < float64(base) {
		d = float64(base)
	}
	if d > float64(max) {
		d = float64(max)
	}
	return time.Duration(d * float64(time.Second))
}

// startCooldownLocked activates a cooldown sized from the current cooldownStreak,
// then increments the streak. Caller must hold h.mu.
func startCooldownLocked(state *model.ChannelHealthState, cfg *operation_setting.ChannelHealthSetting, now time.Time) {
	d := CooldownDuration(cfg, state.CooldownStreak)
	if d <= 0 {
		return
	}
	state.CooldownStreak++
	state.FailureStreak = 0
	state.RequestCount = 0
	state.RampExited = false
	state.CooldownUntil = now.Add(d)
}

// finishCooldownLocked clears an expired cooldown and arms the slow-start ramp.
// Caller must hold h.mu.
func finishCooldownLocked(state *model.ChannelHealthState) {
	state.CooldownUntil = time.Time{}
	state.RequestCount = 0
	state.RampExited = false
	state.RampPending = true
}

// RoutingBaseWeight converts a configured channel weight into the base weight
// used for weighted-random routing.
func RoutingBaseWeight(weight int) uint {
	if weight < 0 {
		return 1
	}
	return uint(weight) + 1
}

// Reset clears all health state. Called when the kill switch is toggled off.
func (h *HealthStore) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = make(map[int]*model.ChannelHealthState)
}

// GetScore returns the current EWMA score for diagnostics.
func (h *HealthStore) GetScore(channelID int) float64 {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return model.DefaultScore
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[channelID]
	if !ok {
		return model.DefaultScore
	}
	return state.EwmaScore
}

// FilterCoolingChannels reports which of channelIDs must be removed from a
// priority tier because they are in a live cooldown.
func (h *HealthStore) FilterCoolingChannels(channelIDs []int, maxEjectionPercent int) map[int]bool {
	ejected := make(map[int]bool)
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled || maxEjectionPercent <= 0 {
		return ejected
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	now := model.ChannelHealthNow()

	var cooling []int
	for _, id := range channelIDs {
		state, ok := h.states[id]
		if !ok || state.CooldownUntil.IsZero() {
			continue
		}
		if !state.CooldownUntil.After(now) {
			finishCooldownLocked(state)
			continue
		}
		cooling = append(cooling, id)
	}
	if len(cooling) == 0 {
		return ejected
	}

	// Fully cooling tier, or an operator asking for total ejection: drop all.
	if len(cooling) == len(channelIDs) || maxEjectionPercent >= 100 {
		for _, id := range cooling {
			ejected[id] = true
		}
		return ejected
	}

	sort.Ints(cooling)

	ejectLimit := len(channelIDs) * maxEjectionPercent / 100 // floor
	if ejectLimit < 1 {
		ejectLimit = 1
	}
	if ejectLimit > len(cooling) {
		ejectLimit = len(cooling)
	}
	for _, id := range cooling[:ejectLimit] {
		ejected[id] = true
	}
	return ejected
}

// SnapshotCooldownStateForTest returns the tracked state of one channel for testing.
func (h *HealthStore) SnapshotCooldownStateForTest(channelID int) (model.CooldownStateSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[channelID]
	if !ok {
		return model.CooldownStateSnapshot{}, false
	}
	return model.CooldownStateSnapshot{
		RequestCount:   state.RequestCount,
		RampPending:    state.RampPending,
		RampExited:     state.RampExited,
		CooldownStreak: state.CooldownStreak,
	}, true
}

// init registers the health store implementation with the model package bridge
// so model-internal consumers keep working without this package importing model
// in the reverse direction.
func init() {
	model.RegisterHealthBridge(model.HealthBridge{
		ClassifyOutcome:       hClassifyOutcome,
		RecordChannelOutcome:  hRecordChannelOutcome,
		RecordRequestAttempts: hRecordRequestAttempts,
		RecordOutcome:         hRecordOutcome,
		EffectiveWeight:       hEffectiveWeight,
		RoutingWeight:         hRoutingWeight,
		CooldownDuration:      CooldownDuration,
		RoutingBaseWeight:     RoutingBaseWeight,
		Reset:                 hReset,
		GetScore:              hGetScore,
		FilterCoolingChannels: hFilterCoolingChannels,
		SnapshotCooldownState: hSnapshotCooldownState,
		ResetForTest:          ResetHealthStoreForTest,
	})
}

func hClassifyOutcome(err *types.NewAPIError, channelID int) model.ChannelOutcome {
	return GetHealthStore().ClassifyChannelOutcome(err, channelID)
}

func hRecordChannelOutcome(channelID int, outcome model.ChannelOutcome) {
	GetHealthStore().RecordChannelOutcome(channelID, outcome)
}

func hRecordRequestAttempts(attempts []model.ChannelAttempt, winnerID int, succeeded bool) {
	GetHealthStore().RecordRequestAttempts(attempts, winnerID, succeeded)
}

func hRecordOutcome(channelID int, success bool) {
	GetHealthStore().RecordOutcome(channelID, success)
}

func hEffectiveWeight(channelID int, baseWeight uint) float64 {
	return GetHealthStore().EffectiveWeight(channelID, baseWeight)
}

func hRoutingWeight(channelID int, baseWeight uint, bypassCooldown bool) float64 {
	return GetHealthStore().RoutingWeight(channelID, baseWeight, bypassCooldown)
}

func hReset() {
	GetHealthStore().Reset()
}

func hGetScore(channelID int) float64 {
	return GetHealthStore().GetScore(channelID)
}

func hFilterCoolingChannels(channelIDs []int, maxEjectionPercent int) map[int]bool {
	return GetHealthStore().FilterCoolingChannels(channelIDs, maxEjectionPercent)
}

func hSnapshotCooldownState(channelID int) (model.CooldownStateSnapshot, bool) {
	return GetHealthStore().SnapshotCooldownStateForTest(channelID)
}