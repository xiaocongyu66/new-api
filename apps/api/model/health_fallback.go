package model

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// health_fallback.go provides the local fallback health scoring implementation
// used when the capability bridge is not registered (e.g., model-only test
// binaries). It mirrors the logic in internal/catalog/health_store.go
// exactly so model package tests pass without linking the capability package.

func classifyChannelOutcomeUnlocked(state *ChannelHealthState, err *types.NewAPIError) ChannelOutcome {
	if err == nil {
		state.UnauthorizedRun = 0
		return OutcomeSuccess
	}

	if types.IsChannelError(err) || err.GetErrorCode() == types.ErrorCodeBadResponseBody {
		state.UnauthorizedRun = 0
		return OutcomeFatal
	}

	if err.StatusCode == 429 {
		state.UnauthorizedRun = 0
		return OutcomeThrottled
	}

	if err.StatusCode >= 500 {
		state.UnauthorizedRun = 0
		return OutcomeFatal
	}

	if err.StatusCode == 401 {
		if state.UnauthorizedRun < UnauthorizedEscalationThreshold {
			state.UnauthorizedRun++
		}
		if state.UnauthorizedRun >= UnauthorizedEscalationThreshold {
			return OutcomeFatal
		}
		return OutcomeNeutral
	}

	state.UnauthorizedRun = 0
	return OutcomeNeutral
}

func (l *localHealthManager) classifyChannelOutcome(err *types.NewAPIError, channelID int) ChannelOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()

	state, tracked := l.states[channelID]
	if !tracked {
		state = &ChannelHealthState{EwmaScore: DefaultScore}
	}

	outcome := classifyChannelOutcomeUnlocked(state, err)

	if !tracked && state.UnauthorizedRun > 0 {
		l.states[channelID] = state
	}

	return outcome
}

func (l *localHealthManager) recordChannelOutcome(channelID int, modelName string, outcome ChannelOutcome) {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.states[channelID]
	if !ok {
		state = &ChannelHealthState{
			EwmaScore:       DefaultScore,
			RequestCount:    0,
			UnauthorizedRun: 0,
		}
		l.states[channelID] = state
	}

	now := ChannelHealthNow()

	if !state.CooldownUntil.IsZero() && !state.CooldownUntil.After(now) {
		l.finishCooldownLocked(state)
	}

	switch outcome {
	case OutcomeSuccess:
		state.FailureStreak = 0
		if state.CooldownStreak > 0 && state.CooldownUntil.IsZero() {
			state.CooldownStreak--
		}
	case OutcomeNeutral:
		state.FailureStreak = 0
		return
	case OutcomeFatal, OutcomeThrottled:
		state.FailureStreak++
	}

	var observation float64
	switch outcome {
	case OutcomeSuccess:
		observation = 1.0
	case OutcomeFatal:
		observation = 0.0
	case OutcomeThrottled:
		observation = ThrottledObservation
	default:
		observation = 0.0
	}

	state.RequestCount++
	state.RampPending = false

	if outcome == OutcomeFatal {
		state.RampExited = true
	}

	if state.RequestCount > cfg.MinRequests {
		state.EwmaScore = cfg.Alpha*observation + (1-cfg.Alpha)*state.EwmaScore
		if state.EwmaScore < cfg.MinScore {
			state.EwmaScore = cfg.MinScore
		}
	}

	if (outcome == OutcomeFatal || outcome == OutcomeThrottled) && state.FailureStreak >= cfg.CooldownThreshold {
		l.startCooldownLocked(state, cfg, now)
		if modelName != "" {
			l.escalateModelLocked(state, cfg, channelID, modelName)
		}
	}
}

func (l *localHealthManager) escalateModelLocked(state *ChannelHealthState, cfg *operation_setting.ChannelHealthSetting, channelID int, modelName string) {
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
	disable := channelModelDisabler
	go func() {
		if err := disable(channelID, modelName); err != nil {
			common.SysError(fmt.Sprintf("failed to disable model %q on channel %d after repeated cooldowns: %s",
				modelName, channelID, err.Error()))
			l.mu.Lock()
			defer l.mu.Unlock()
			if current, ok := l.states[channelID]; ok {
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

func (l *localHealthManager) recordRequestAttempts(attempts []ChannelAttempt, winnerID int, succeeded bool) {
	if succeeded {
		l.recordChannelOutcome(winnerID, "", l.classifyChannelOutcome(nil, winnerID))
		return
	}
	for _, attempt := range attempts {
		if attempt.Outcome.AffectsHealth() {
			l.recordChannelOutcome(attempt.ChannelID, attempt.ModelName, attempt.Outcome)
		}
	}
}

func (l *localHealthManager) recordOutcome(channelID int, success bool) {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.states[channelID]
	if !ok {
		state = &ChannelHealthState{
			EwmaScore:       DefaultScore,
			RequestCount:    0,
			UnauthorizedRun: 0,
		}
		l.states[channelID] = state
	}

	state.RequestCount++

	if !success {
		state.RampExited = true
	}

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

func (l *localHealthManager) routingWeight(channelID int, baseWeight uint, bypassCooldown bool) float64 {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return float64(baseWeight)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.states[channelID]
	if !ok {
		return float64(baseWeight)
	}

	now := ChannelHealthNow()
	if !state.CooldownUntil.IsZero() {
		if state.CooldownUntil.After(now) {
			if !bypassCooldown {
				return 0
			}
		} else {
			l.finishCooldownLocked(state)
		}
	}

	return float64(baseWeight) * state.EwmaScore * slowStartFactor(state, cfg.MinRequests)
}

func (l *localHealthManager) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states = make(map[int]*ChannelHealthState)
}

func (l *localHealthManager) getScore(channelID int) float64 {
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled {
		return DefaultScore
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.states[channelID]
	if !ok {
		return DefaultScore
	}
	return state.EwmaScore
}

func (l *localHealthManager) filterCoolingChannels(channelIDs []int, maxEjectionPercent int) map[int]bool {
	ejected := make(map[int]bool)
	cfg := operation_setting.GetChannelHealthSetting()
	if cfg == nil || !cfg.Enabled || maxEjectionPercent <= 0 {
		return ejected
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := ChannelHealthNow()

	var cooling []int
	for _, id := range channelIDs {
		state, ok := l.states[id]
		if !ok || state.CooldownUntil.IsZero() {
			continue
		}
		if !state.CooldownUntil.After(now) {
			l.finishCooldownLocked(state)
			continue
		}
		cooling = append(cooling, id)
	}
	if len(cooling) == 0 {
		return ejected
	}

	if len(cooling) == len(channelIDs) || maxEjectionPercent >= 100 {
		for _, id := range cooling {
			ejected[id] = true
		}
		return ejected
	}

	sort.Ints(cooling)

	ejectLimit := len(channelIDs) * maxEjectionPercent / 100
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

func (l *localHealthManager) snapshotCooldownState(channelID int) (CooldownStateSnapshot, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.states[channelID]
	if !ok {
		return CooldownStateSnapshot{}, false
	}
	return CooldownStateSnapshot{
		RequestCount:   state.RequestCount,
		RampPending:    state.RampPending,
		RampExited:     state.RampExited,
		CooldownStreak: state.CooldownStreak,
	}, true
}

func (l *localHealthManager) startCooldownLocked(state *ChannelHealthState, cfg *operation_setting.ChannelHealthSetting, now time.Time) {
	d := cooldownDurationCalc(cfg, state.CooldownStreak)
	if d <= 0 {
		return
	}
	state.CooldownStreak++
	state.FailureStreak = 0
	state.RequestCount = 0
	state.RampExited = false
	state.CooldownUntil = now.Add(d)
}

func (l *localHealthManager) finishCooldownLocked(state *ChannelHealthState) {
	state.CooldownUntil = time.Time{}
	state.RequestCount = 0
	state.RampExited = false
	state.RampPending = true
}

func cooldownDurationCalc(cfg *operation_setting.ChannelHealthSetting, priorActivations int) time.Duration {
	base, max := cfg.CooldownBaseSeconds, cfg.CooldownMaxSeconds
	if base <= 0 || max <= 0 {
		return 0
	}
	if max < base {
		max = base
	}
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

// Ensure unused imports are referenced.
var _ = sync.Once{}
